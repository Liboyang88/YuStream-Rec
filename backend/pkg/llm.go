package pkg

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type LLMClient struct {
	httpClient *http.Client
	ApiKey    string
	ModelName string
}

func NewLLMClient(apiKey string, modelName string) *LLMClient {
	return &LLMClient{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		ApiKey:    apiKey,
		ModelName: modelName,
	}
}

// ---- 请求体结构化类型 ----
// 请求体统一用 struct + json.Marshal 构造，由标准库负责 JSON 转义，
// 不再手工拼接 JSON 字符串，避免多行 prompt / 用户数据中的换行、
// 引号等控制字符导致请求体非法。

// ChatMessage 一条对话消息。
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ThinkingConfig 思考模式配置。
type ThinkingConfig struct {
	Type string `json:"type"`
}

// ToolFunction 工具定义中的 function 部分。
// Parameters 为任意可序列化对象（通常是 JSON Schema 结构体），
// 序列化时由 json.Marshal 统一处理。
type ToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  ToolParameters `json:"parameters"`
}

type ToolParameters struct {
	Type       string                 `json:"type"`
	Required   []string               `json:"required"`
	Properties map[string]any         `json:"properties"`
}

type CommonField struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type FieldWithItems struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Items any `json:"items"`
}

// Tool 一个 function 工具。
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolChoiceFunction tool_choice 中指定的函数名。
type ToolChoiceFunction struct {
	Name string `json:"name"`
}

// ToolChoice 强制模型调用指定工具。
type ToolChoice struct {
	Type     string             `json:"type"`
	Function ToolChoiceFunction `json:"function"`
}

// ChatRequest 对应 POST /chat/completions 的请求体。
type ChatRequest struct {
	Model       string          `json:"model"`
	Messages    []ChatMessage   `json:"messages"`
	Thinking    *ThinkingConfig `json:"thinking,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	TopP        float64         `json:"top_p,omitempty"`
	Stream      bool            `json:"stream"`
	Tools       []Tool          `json:"tools,omitempty"`
	Strict      bool            `json:"strict,omitempty"`
	ToolChoice  *ToolChoice     `json:"tool_choice,omitempty"`
	// UserID      string          `json:"user_id,omitempty"`
}

// ---- 响应类型 ----

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type ToolCall struct {
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Function FunctionCall `json:"function"`
}
type Message struct {
	Role            string     `json:"role"`
	Content         string     `json:"content"`
	ReasoningContent string    `json:"reasoning_content"`
	ToolCalls       []ToolCall `json:"tool_calls"`
}
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}
type Usage struct {
	PromptTokens          int `json:"prompt_tokens"`
	CompletionTokens      int `json:"completion_tokens"`
	TotalTokens           int `json:"total_tokens"`
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
}
type RespData struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

func (cli *LLMClient) Send(
	ctx context.Context, 
	messages []ChatMessage, tools []Tool, 
	toolChoice *ToolChoice, resp *RespData) error {
	
	url := "https://api.deepseek.com/chat/completions"
	method := "POST"

	reqBody := ChatRequest{
		Model:       cli.ModelName,
		Messages:    messages,
		Thinking:    &ThinkingConfig{Type: "disabled"},
		MaxTokens:   4096,
		Temperature: 1,
		TopP:        1,
		Stream:      false,
		Tools:       tools,
		Strict:      true,
		ToolChoice:  toolChoice,
	}

	payloadBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("构造 LLM 请求体失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return err
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", cli.ApiKey))

	res, err := cli.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("读取 LLM 响应失败: %w", err)
	}

	// 非 2xx：直接透传 API 返回的原始错误文本，不要把错误响应体当 JSON 解析，
	// 否则真实错误会被 "invalid character 'F'..." 之类的解析错误掩盖。
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("LLM API 返回 %s: %s", res.Status, string(body))
	}

	fmt.Println(string(body))
	if err := json.Unmarshal(body, resp); err != nil {
		return fmt.Errorf("解析 LLM 响应失败: %w, 响应体: %s", err, string(body))
	}
	return nil
}
