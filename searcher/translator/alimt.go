package translator

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"searcher/retry"
	"searcher/semaphore"
)

// 阿里云机器翻译通用版（alimt TranslateGeneral）的实现。
//
// 与 Qwen-MT 的差别：
//
//   - 认证方式：RPC 风格 OpenAPI，用 RAM 的 AccessKey ID/Secret 做 HMAC-SHA1 签名，
//     而不是 Bearer + ApiKey；请求体是 form 表单而不是 JSON，
//     译文在 Data.Translated 而不是 choices[0].message.content。
//   - 额度：文档写明 **QPS 限制 50**，比 qwen-mt-lite 的 60 RPM 高两个数量级，
//     因此默认不必限成"每秒一个"，可以按并发闸门 + 速率放行一起跑。
//   - 长度上限：单次请求 **5000 字符**，超长会返回 10008。
//     论文摘要绝大多数在几百字符，但个别长摘要不能整条失败，
//     因此做了分段翻译兜底（见 segment.go）。
const (
	// alimtDefaultEndpoint 是机器翻译通用版的接入地址（华东 1 杭州）。
	// 其他地域（如新加坡 ap-southeast-1）需换成对应的 mt.<region>.aliyuncs.com。
	alimtDefaultEndpoint = "https://mt.cn-hangzhou.aliyuncs.com/"

	// apiVersion / apiAction 是 OpenAPI 的版本与操作名，签名与请求都要带上。
	apiVersion = "2018-10-12"
	apiAction  = "TranslateGeneral"

	// defaultScene 是通用版固定的场景值。
	defaultScene = "general"
)

// 签名相关的固定值（阿里云 RPC 风格 OpenAPI 的约定）。
const (
	signatureMethod  = "HMAC-SHA1"
	signatureVersion = "1.0"
	httpMethod       = http.MethodPost
)

// ---- 响应体 ----
//
// 正常返回：{"Code":"200","Message":"success","RequestId":"...","Data":{"Translated":"...","WordCount":"10"}}
// 异常返回：{"Code":"10005","Message":"The specified language pair is not supported.","RequestId":"..."}
//
// 注意 Code 的类型：接口文档把它写成 integer，**实测真实响应是字符串**（"200"）。
// 因此用 codeValue 兼容两种形态，不能直接声明成 int——那样会以
// "cannot unmarshal string into Go struct field ... of type int" 解析失败，
// 把一次成功的翻译误判成失败并白白重试 4 次。
type alimtData struct {
	Translated       string `json:"Translated"`
	WordCount        string `json:"WordCount"`
	DetectedLanguage string `json:"DetectedLanguage"`
}

type alimtResponse struct {
	Code      codeValue `json:"Code"`
	Message   string    `json:"Message"`
	RequestID string    `json:"RequestId"`
	Data      alimtData `json:"Data"`
}

// codeValue 是接口错误码，兼容 JSON 里的数字（200）与字符串（"200"）两种写法。
type codeValue int

// UnmarshalJSON 接受数字、字符串；空/null 当作 0（下面会按"非成功码"处理）。
func (c *codeValue) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*c = 0
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("无法解析错误码 %s: %w", b, err)
	}
	*c = codeValue(n)
	return nil
}

// successCode 是接口约定的成功错误码。
const successCode = 200

// permanentErrorCodes 是重试没有意义的错误码：参数缺失、语种不支持、权限/欠费问题。
// 其余（超时、系统错误、翻译失败、QPS 限流）都是临时状况，交给上层退避重试。
var permanentErrorCodes = map[int]bool{
	10004: true, // Some required parameters are missing.
	10005: true, // The specified language pair is not supported.
	10008: true, // The specified string is too long.（已按 5000 字符分段，仍然超长说明不是瞬时问题）
	10009: true, // 子账号没有权限
	10010: true, // 账号没有开通服务
	10013: true, // 账号服务没有开通或者欠费
}

// alimtClient 是机器翻译通用版客户端。
//
// 在多个 goroutine 间共享是安全的：除 gate / limiter 外没有可变状态，
// 两者自身都带锁语义，http.Client 也允许并发使用。
// 批量翻译时靠 gate 控制同时在飞的请求数、靠 limiter 控制发出的节奏。
type alimtClient struct {
	httpClient *http.Client
	endpoint   string
	accessKey  string
	secretKey  string
	glossary   []GlossaryItem
	maxLength  int
	retry      retry.Config
	gate       *semaphore.Semaphore
	limiter    *semaphore.Limiter

	// now / nonce 便于测试替换，生产环境用真实时间与随机串。
	now   func() time.Time
	nonce func() string
}

// newAlimtClient 按共用可选项构造 alimt 客户端。
// accessKey / secretKey 是 RAM 账号的 AccessKey ID 与 Secret（不是百炼的 ApiKey）。
func newAlimtClient(accessKey, secretKey string, o options) *alimtClient {
	endpoint := o.endpoint
	if endpoint == "" {
		endpoint = alimtDefaultEndpoint
	}
	now := o.now
	if now == nil {
		now = time.Now
	}
	nonce := o.nonce
	if nonce == nil {
		nonce = randomNonce
	}
	return &alimtClient{
		httpClient: o.httpClient,
		endpoint:   endpoint,
		accessKey:  accessKey,
		secretKey:  secretKey,
		glossary:   o.glossary,
		maxLength:  o.maxTextLength,
		retry:      o.retry,
		gate:       o.gate,
		limiter:    o.limiter,
		now:        now,
		nonce:      nonce,
	}
}

// Translate 翻译单段文本，超长时自动分段。
// 调用失败会按客户端配置重试，默认失败后再试 3 次；全部失败才返回最后一次的错误。
func (c *alimtClient) Translate(ctx context.Context, text, srcLang, dstLang string) (string, error) {
	segments := splitSegments(text, pairSeparator, c.maxLength)
	translated := make([]string, 0, len(segments))
	for _, segment := range segments {
		out, err := c.translateWithRetry(ctx, segment, srcLang, dstLang)
		if err != nil {
			return "", err
		}
		translated = append(translated, out)
	}
	return strings.Join(translated, ""), nil
}

// TranslateItem 把标题与摘要翻译成中文（合并为一次调用，失败时降级）。
func (c *alimtClient) TranslateItem(ctx context.Context, title, abstract string) (string, string, error) {
	return translateItem(ctx, c, title, abstract, func(text string) []string {
		return splitSegments(text, pairSeparator, c.maxLength)
	})
}

// translateWithRetry 翻译一段文本（含重试），并按术语表在请求前后做替换。
func (c *alimtClient) translateWithRetry(ctx context.Context, text, srcLang, dstLang string) (string, error) {
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
// 先按速率排队（限流器），再抢并发名额（闸门）：两者都不占用等待重试退避的时间，
// 因此退避中的 goroutine 不会白占名额。
func (c *alimtClient) translateOnce(ctx context.Context, text, srcLang, dstLang string) (string, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return "", err
	}
	if err := c.gate.Acquire(ctx); err != nil {
		return "", err
	}
	defer c.gate.Release()

	params := c.buildParams(text, srcLang, dstLang)
	c.sign(params)

	req, err := http.NewRequestWithContext(ctx, httpMethod, c.endpoint, strings.NewReader(params.Encode()))
	if err != nil {
		return "", fmt.Errorf("构造翻译请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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
		// 4xx（限流 429 除外）是请求本身的问题，重试无意义，直接上报。
		if res.StatusCode >= 400 && res.StatusCode < 500 && res.StatusCode != http.StatusTooManyRequests {
			return "", retry.Permanent(err)
		}
		return "", err
	}

	var resp alimtResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("解析翻译响应失败: %w, 原始内容: %s", err, body)
	}
	if int(resp.Code) != successCode {
		err := fmt.Errorf("翻译接口返回错误 %d: %s", int(resp.Code), resp.Message)
		if permanentErrorCodes[int(resp.Code)] {
			return "", retry.Permanent(err)
		}
		return "", err
	}
	if strings.TrimSpace(resp.Data.Translated) == "" {
		return "", fmt.Errorf("翻译接口返回空结果: %s", body)
	}
	return resp.Data.Translated, nil
}

// buildParams 组装一次调用的全部请求参数（不含签名）。
func (c *alimtClient) buildParams(text, srcLang, dstLang string) url.Values {
	params := url.Values{}
	// 公共参数
	params.Set("Format", "JSON")
	params.Set("Version", apiVersion)
	params.Set("AccessKeyId", c.accessKey)
	params.Set("SignatureMethod", signatureMethod)
	params.Set("SignatureVersion", signatureVersion)
	params.Set("SignatureNonce", c.nonce())
	params.Set("Timestamp", c.now().UTC().Format("2006-01-02T15:04:05Z"))

	// 业务参数
	params.Set("Action", apiAction)
	params.Set("FormatType", "text")
	params.Set("Scene", defaultScene)
	params.Set("SourceText", text)
	if code := normalizeLang(srcLang); code != "" {
		params.Set("SourceLanguage", code)
	}
	params.Set("TargetLanguage", normalizeLang(dstLang))
	return params
}

// sign 按阿里云 RPC 风格 OpenAPI 的规则计算签名并写入 params["Signature"]。
//
// 规则（顺序不能变）：
//  1. 除 Signature 外的所有参数按参数名做字典序排序；
//  2. 用 RFC3986（= url.QueryEscape，空格为 + 需还原成 %20）编码 k/v 后拼成
//     "k1=v1&k2=v2" 形式的规范化查询串，签名与发送的 body 共用这一套编码；
//  3. StringToSign = HTTPMethod + "&" + percentEncode("/") + "&" + percentEncode(规范化查询串)；
//  4. Signature = Base64(HMAC-SHA1(AccessKeySecret + "&", StringToSign))。
func (c *alimtClient) sign(params url.Values) {
	canonical := canonicalQuery(params)
	stringToSign := httpMethod + "&" + percentEncode("/") + "&" + percentEncode(canonical)

	mac := hmac.New(sha1.New, []byte(c.secretKey+"&"))
	mac.Write([]byte(stringToSign))
	params.Set("Signature", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
}

// canonicalQuery 把参数拼成规范化查询串（不含 Signature，按参数名字典序）。
func canonicalQuery(params url.Values) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "Signature" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(percentEncode(k))
		sb.WriteByte('=')
		sb.WriteString(percentEncode(params.Get(k)))
	}
	return sb.String()
}

// percentEncode 按 RFC3986 编码：url.QueryEscape 的结果再把 "+" 还原成 "%20"，
// 并把 "*" 还原成 "%2A"（否则签名与网关算出来的不一致）。
func percentEncode(s string) string {
	encoded := url.QueryEscape(s)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "*", "%2A")
	return encoded
}

// randomNonce 生成签名用的随机串。接口要求每次请求的 SignatureNonce 都不相同，
// 否则会被判定为重放而拒绝。
func randomNonce() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand 失败极罕见；退化成时间戳也要保证请求能发出去。
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}
