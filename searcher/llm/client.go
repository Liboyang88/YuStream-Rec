// Package llm 提供 OpenAI 兼容的 chat 客户端，以及基于它的文献介绍（summary）生成。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"searcher/retry"
	"searcher/semaphore"
)

const (
	// defaultEndpoint 是千问（qianwenai MaaS）的 OpenAI 兼容接口。
	// 文献介绍改用 qwen3.8-flash 后就不再走 DeepSeek 的地址了。
	defaultEndpoint = "https://maas.qianwenaiapi.com/compatible-mode/v1/chat/completions"

	requestTimeout = 60 * time.Second

	// DefaultSummaryMaxTokens 是文献介绍的输出上限（token）。
	// 提示词里要求"500 token 以内"，这里再给一个硬上限：
	// 一是防止个别条目把额度花在冗长输出上（服务的 TPM 是每分钟 5M，
	// 输出翻倍会直接压缩可并发的请求数），二是让成本可预期。
	DefaultSummaryMaxTokens = 1024
)

// ---- 请求体 ----

// ChatMessage 一条对话消息。
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ThinkingConfig 思考模式配置。
type ThinkingConfig struct {
	Type string `json:"type"`
}

// ChatRequest 对应 POST /chat/completions 的请求体。
//
// 文献介绍已改为"提示词约束输出 + 直接读 message.content"，
// 因此这里不再有 tools / tool_choice / strict 字段。
type ChatRequest struct {
	Model       string          `json:"model"`
	Messages    []ChatMessage   `json:"messages"`
	Thinking    *ThinkingConfig `json:"thinking,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	TopP        float64         `json:"top_p,omitempty"`
	Stream      bool            `json:"stream"`
}

// ---- 响应体 ----

// Message 响应中的一条消息。
//
// 不用 tool calling 之后只需要 Content；ReasoningContent 仍然保留，
// 用于在"模型把内容输出到思考字段"这种异常情况下给出可排查的错误信息
// （思考模式已显式关闭，正常不会出现）。
type Message struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content"`
}

// Choice 响应中的一个候选。
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage token 用量。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// RespData 对应 /chat/completions 的响应体。
type RespData struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Client 是 OpenAI 兼容接口的 chat 客户端。
//
// Client 在多个 goroutine 间共享是安全的：除 gate 外没有可变状态，gate 自身带锁语义，
// http.Client 也允许并发使用。批量富化时靠 gate 控制同时在飞的请求数。
type Client struct {
	httpClient *http.Client
	endpoint   string
	apiKey     string
	modelName  string
	maxTokens  int
	retry      retry.Config
	gate       *semaphore.Semaphore
}

// Option 用于覆盖 Client 的默认行为。
type Option func(*Client)

// WithEndpoint 覆盖接口地址（测试用）。
func WithEndpoint(endpoint string) Option {
	return func(c *Client) { c.endpoint = endpoint }
}

// WithHTTPClient 覆盖底层 HTTP 客户端。
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithMaxTokens 覆盖输出的 token 上限。
func WithMaxTokens(n int) Option {
	return func(c *Client) {
		if n > 0 {
			c.maxTokens = n
		}
	}
}

// WithRetryConfig 覆盖重试策略（默认：失败后再重试 3 次，退避 2s/4s/6s）。
func WithRetryConfig(cfg retry.Config) Option {
	return func(c *Client) { c.retry = cfg }
}

// WithSemaphore 限制同时在飞的请求数（nil 表示不限流）。
// 批量处理多篇论文时，所有请求都共用同一个闸门，避免并发数超出接口的 TPM / QPS 限额。
func WithSemaphore(gate *semaphore.Semaphore) Option {
	return func(c *Client) { c.gate = gate }
}

// NewClient 构造 chat 客户端。
func NewClient(apiKey, modelName string, opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{Timeout: requestTimeout},
		endpoint:   defaultEndpoint,
		apiKey:     apiKey,
		modelName:  modelName,
		maxTokens:  DefaultSummaryMaxTokens,
		retry:      retry.DefaultConfig(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Send 发起一次 chat 请求。
//
// 请求体统一用 struct + json.Marshal 构造，由标准库负责 JSON 转义，
// 避免多行 prompt / 用户数据中的换行、引号等控制字符导致请求体非法。
//
// 配置了闸门时，这里只占用"一次网络往返"的名额：等待重试退避的 goroutine 不占名额。
func (c *Client) Send(ctx context.Context, messages []ChatMessage) (*RespData, error) {
	if err := c.gate.Acquire(ctx); err != nil {
		return nil, err
	}
	defer c.gate.Release()

	payload, err := json.Marshal(ChatRequest{
		Model:    c.modelName,
		Messages: messages,
		// 显式关闭思考模式：文献介绍是抽取式概括，不需要思维链，
		// 开着只会把 TPM 额度花在 reasoning token 上（qwen3.8-flash 的最大思维链是 262K）。
		Thinking:    &ThinkingConfig{Type: "disabled"},
		MaxTokens:   c.maxTokens,
		Temperature: 0.3,
		TopP:        0.8,
		Stream:      false,
	})
	if err != nil {
		return nil, fmt.Errorf("构造 LLM 请求体失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 LLM 接口失败: %w", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 LLM 响应失败: %w", err)
	}

	// 非 2xx：直接透传 API 返回的原始错误文本，不要把错误响应体当 JSON 解析，
	// 否则真实错误会被 "invalid character 'F'..." 之类的解析错误掩盖。
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		err := fmt.Errorf("LLM API 返回 %s: %s", res.Status, body)
		// 4xx 基本是请求本身的问题（key 无效、模型名不存在、参数非法），重试无意义；
		// 429（限流）属于临时状况，仍然交给上层重试。
		if res.StatusCode >= 400 && res.StatusCode < 500 && res.StatusCode != http.StatusTooManyRequests {
			return nil, retry.Permanent(err)
		}
		return nil, err
	}

	resp := &RespData{}
	if err := json.Unmarshal(body, resp); err != nil {
		return nil, fmt.Errorf("解析 LLM 响应失败: %w, 响应体: %s", err, body)
	}
	return resp, nil
}
