package translator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"searcher/retry"
	"searcher/semaphore"
)

// 阿里云百炼 Qwen-MT 的实现（OpenAI 兼容接口）。
//
// 这是换用 alimt 之前使用的那套翻译，保留它的原因是：
//
//   - 对术语与长句的处理比通用版好，适合小批量重译或对译文质量要求高的场景；
//   - 代价是额度低得多：qwen-mt-lite 只有 **60 RPM（≈1 RPS）**。
//
// 因此**并发与限速按 Qwen-MT 的口径单独默认**（见 translator.go 的 New）：
// 每秒放行 1 个请求，且条目级并发压到远低于 alimt 的水平。
// 只把并发数压小是不够的——并发闸门管不住"发出的节奏"，并发 8、每次 0.3s
// 仍会在 300ms 内连发 8 个，而百炼对"请求频率短时间激增"有额外保护
// （返回 "Request rate increased too quickly"），所以限速器必须一起上。
const (
	// qwenDefaultEndpoint 是百炼 OpenAI 兼容模式的 chat 接口。
	qwenDefaultEndpoint = "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"

	// qwenDefaultModel 是 Qwen-MT 轻量版翻译模型。
	qwenDefaultModel = "qwen-mt-lite"

	// DefaultQwenQPS / DefaultQwenBurst 是 Qwen-MT 的默认限速：
	// 60 RPM 折算成"每秒 1 个请求、不突发"。
	// 这两个默认值只在调用方没有显式配置时兜底，见 New。
	DefaultQwenQPS   = 1.0
	DefaultQwenBurst = 1

	// DefaultQwenConcurrency 是 Qwen-MT 默认的条目级并发上限。
	// 额度只有 1 RPS，并发开大只会让请求在闸门里排队，还更容易触发突发保护。
	DefaultQwenConcurrency = 2
)

// ---- 请求体 ----

type qwenTranslationOptions struct {
	SourceLang string         `json:"source_lang"`
	TargetLang string         `json:"target_lang"`
	Glossary   []GlossaryItem `json:"glossary,omitempty"`
}

type qwenMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type qwenRequest struct {
	Model              string                 `json:"model"`
	Messages           []qwenMessage          `json:"messages"`
	TranslationOptions qwenTranslationOptions `json:"translation_options"`
}

// ---- 响应体 ----

type qwenResponse struct {
	Choices []struct {
		Message qwenMessage `json:"message"`
	} `json:"choices"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// qwenClient 是 Qwen-MT 翻译客户端。
//
// 在多个 goroutine 间共享是安全的：除 gate / limiter 外没有可变状态，
// 两者自身都带锁语义，http.Client 也允许并发使用。
type qwenClient struct {
	httpClient *http.Client
	endpoint   string
	apiKey     string
	model      string
	glossary   []GlossaryItem
	retry      retry.Config
	gate       *semaphore.Semaphore
	limiter    *semaphore.Limiter
}

// newQwenClient 按共用可选项构造 Qwen-MT 客户端。
// apiKey 是百炼（DashScope）的 ApiKey。
func newQwenClient(apiKey string, o options) *qwenClient {
	endpoint := o.endpoint
	if endpoint == "" {
		endpoint = qwenDefaultEndpoint
	}
	model := o.model
	if model == "" {
		model = qwenDefaultModel
	}
	return &qwenClient{
		httpClient: o.httpClient,
		endpoint:   endpoint,
		apiKey:     apiKey,
		model:      model,
		glossary:   o.glossary,
		retry:      o.retry,
		gate:       o.gate,
		limiter:    o.limiter,
	}
}

// Translate 翻译单段文本。
//
// Qwen-MT 没有单次字符数上限（不接口 5000 那种硬限制），所以整段发一次；
// 分段函数返回单个元素，让共用的 TranslateItem 走同一条代码路径。
func (c *qwenClient) Translate(ctx context.Context, text, srcLang, dstLang string) (string, error) {
	return c.translateWithRetry(ctx, text, srcLang, dstLang)
}

// TranslateItem 把标题与摘要翻译成中文（合并为一次调用，失败时降级）。
func (c *qwenClient) TranslateItem(ctx context.Context, title, abstract string) (string, string, error) {
	return translateItem(ctx, c, title, abstract, func(text string) []string {
		return []string{text} // 不分段
	})
}

// translateWithRetry 翻译一段文本（含重试），并按术语表在请求前后做替换。
func (c *qwenClient) translateWithRetry(ctx context.Context, text, srcLang, dstLang string) (string, error) {
	masked, restore := maskGlossary(text, c.glossary)

	var translated string
	err := retry.Do(ctx, c.retry, "翻译", func() error {
		out, err := c.translateOnce(ctx, masked, srcLang, dstLang)
		if err != nil {
			return err
		}
		translated = out
		return nil
	})
	if err != nil {
		return "", err
	}
	return restore(translated), nil
}

// translateOnce 发起一次翻译请求（不含重试）。
//
// 先按速率排队（限流器），再抢并发名额（闸门）：Qwen-MT 的额度只有 1 RPS，
// 这一步是它能不能稳定跑下去的关键，退避中的 goroutine 不会白占名额。
func (c *qwenClient) translateOnce(ctx context.Context, text, srcLang, dstLang string) (string, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return "", err
	}
	if err := c.gate.Acquire(ctx); err != nil {
		return "", err
	}
	defer c.gate.Release()

	reqBody := qwenRequest{
		Model:    c.model,
		Messages: []qwenMessage{{Role: "user", Content: text}},
		TranslationOptions: qwenTranslationOptions{
			SourceLang: normalizeQwenLang(srcLang),
			TargetLang: normalizeQwenLang(dstLang),
			// 术语表由客户端占位符方案处理（与 alimt 一致），这里不再重复下发，
			// 避免"原生 glossary + 占位符"两套机制互相干扰。
		},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("构造翻译请求体失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("构造翻译请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求翻译接口失败: %w", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("读取翻译响应失败: %w", err)
	}

	// 非 2xx 直接透传原始响应，避免被 JSON 解析错误掩盖真实原因
	//（例如网关返回 HTML 时会报 "invalid character '<'"）。
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		err := fmt.Errorf("翻译接口返回 %s: %s", res.Status, body)
		// 4xx（限流 429 除外）是请求本身的问题（key 无效、模型名不存在、参数非法），
		// 重试无意义，直接上报。
		if res.StatusCode >= 400 && res.StatusCode < 500 && res.StatusCode != http.StatusTooManyRequests {
			return "", retry.Permanent(err)
		}
		return "", err
	}

	var resp qwenResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("解析翻译响应失败: %w, 原始内容: %s", err, body)
	}
	if resp.Error.Code != "" {
		return "", fmt.Errorf("翻译接口返回错误: %s: %s", resp.Error.Code, resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("翻译接口返回空结果: %s", body)
	}
	return resp.Choices[0].Message.Content, nil
}

// normalizeQwenLang 把语言标识转成 Qwen-MT 需要的全称（Chinese / English）。
// Qwen-MT 的 translation_options 用的是 {"source_lang":"auto","target_lang":"Chinese"}
// 这种带全称的写法，与 alimt 的短码 zh/en 不同。
func normalizeQwenLang(lang string) string {
	switch normalizeLang(lang) {
	case LangAuto:
		return LangAuto
	case "zh":
		return "Chinese"
	case "en":
		return "English"
	default:
		return strings.TrimSpace(lang)
	}
}
