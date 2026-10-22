package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"searcher/retry"
	"searcher/semaphore"
)

// testModel 是与 .env 里 MODEL_NAME 对齐的模型名（千问 MaaS 上的 qwen3.8-flash）。
const testModel = "qwen3.8-flash"

// testRetry 使用零退避的重试策略，避免失败用例真的等待 2s/4s/6s。
func testRetry() Option {
	return WithRetryConfig(retry.Config{MaxRetries: 3, Backoff: 0})
}

// writeContent 返回一段 chat 响应。不用 tool calling 之后，
// 文献介绍直接在 message.content 里，finishReason 用于模拟被截断的情况。
func writeContent(w http.ResponseWriter, content, finishReason string) {
	w.Header().Set("Content-Type", "application/json")
	msg := map[string]any{"role": "assistant", "content": content}
	choice := map[string]any{"message": msg}
	if finishReason != "" {
		choice["finish_reason"] = finishReason
	}
	json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{choice}})
}

// writeOK 返回一段正常的文献介绍。
func writeOK(w http.ResponseWriter, content string) { writeContent(w, content, "stop") }

// TestSendRequestShape 校验请求体形态：模型名、鉴权头、消息结构，
// 并确认不再发送任何 tool calling 相关字段。
func TestSendRequestShape(t *testing.T) {
	var (
		gotAuth string
		gotReq  ChatRequest
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, err.Error(), 400)
			return
		}
		writeOK(w, "这是一段文献介绍")
	}))
	defer srv.Close()

	s := NewSummarizer("test-key", testModel, WithEndpoint(srv.URL), testRetry())
	summary, err := s.GenerateSummary(context.Background(), "A Title", "An Abstract")
	if err != nil {
		t.Fatalf("GenerateSummary: %v", err)
	}
	if summary != "这是一段文献介绍" {
		t.Errorf("summary = %q", summary)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotReq.Model != testModel {
		t.Errorf("model = %q, want %q", gotReq.Model, testModel)
	}

	// 两条消息：system（提示词约束输出） + user（标题与摘要）。
	if len(gotReq.Messages) != 2 {
		t.Fatalf("messages 条数 = %d, want 2", len(gotReq.Messages))
	}
	if gotReq.Messages[0].Role != "system" || gotReq.Messages[1].Role != "user" {
		t.Errorf("消息角色 = %q/%q, want system/user",
			gotReq.Messages[0].Role, gotReq.Messages[1].Role)
	}
	if !strings.Contains(gotReq.Messages[1].Content, "A Title") ||
		!strings.Contains(gotReq.Messages[1].Content, "An Abstract") {
		t.Errorf("user 消息应含标题与摘要，实际 %q", gotReq.Messages[1].Content)
	}

	// 思考模式必须显式关闭：开着只会把 TPM 额度花在 reasoning token 上。
	if gotReq.Thinking == nil || gotReq.Thinking.Type != "disabled" {
		t.Errorf("thinking = %+v, want disabled", gotReq.Thinking)
	}
	if gotReq.Stream {
		t.Error("stream 应为 false（一次性取回结果）")
	}
}

// TestPromptConstrainsOutputFormat 提示词必须承担原先 tool calling 的"约束输出"职责：
// 明确要求只输出正文、禁止 Markdown / JSON / 代码块。
func TestPromptConstrainsOutputFormat(t *testing.T) {
	for _, want := range []string{
		"只输出一段连续文本",
		"不要任何前后缀",
		"不要使用 Markdown",
		"不要输出 JSON",
		"```",
		"禁止幻觉",
	} {
		if !strings.Contains(promptGenerateSummary, want) {
			t.Errorf("提示词缺少输出约束 %q", want)
		}
	}
}

// TestRequestHasNoTools 请求里不该再出现 tools / tool_choice 字段。
func TestRequestHasNoTools(t *testing.T) {
	var raw map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Errorf("decode request: %v", err)
		}
		writeOK(w, "介绍")
	}))
	defer srv.Close()

	s := NewSummarizer("test-key", testModel, WithEndpoint(srv.URL), testRetry())
	if _, err := s.GenerateSummary(context.Background(), "T", "A"); err != nil {
		t.Fatalf("GenerateSummary: %v", err)
	}
	for _, key := range []string{"tools", "tool_choice", "strict"} {
		if _, ok := raw[key]; ok {
			t.Errorf("请求体不该包含 %q 字段（已改为提示词约束输出）", key)
		}
	}
}

// TestMaxTokensIsCapped 输出上限要有值，避免个别条目把 TPM 额度花在冗长输出上。
func TestMaxTokensIsCapped(t *testing.T) {
	var gotReq ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Errorf("decode request: %v", err)
		}
		writeOK(w, "介绍")
	}))
	defer srv.Close()

	s := NewSummarizer("test-key", testModel, WithEndpoint(srv.URL), testRetry())
	if _, err := s.GenerateSummary(context.Background(), "T", "A"); err != nil {
		t.Fatalf("GenerateSummary: %v", err)
	}
	if gotReq.MaxTokens != DefaultSummaryMaxTokens {
		t.Errorf("max_tokens = %d, want %d", gotReq.MaxTokens, DefaultSummaryMaxTokens)
	}
}

// TestGenerateSummaryAPIError 非 2xx 时错误信息应包含响应体原文。
func TestGenerateSummaryAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	s := NewSummarizer("bad-key", testModel, WithEndpoint(srv.URL), testRetry())
	if _, err := s.GenerateSummary(context.Background(), "T", "A"); err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
}

// TestExtractSummary 从 message.content 里取正文，并剥掉模型偶尔加的包装。
func TestExtractSummary(t *testing.T) {
	cases := []struct {
		name    string
		resp    *RespData
		want    string
		wantErr bool
	}{
		{
			name: "正常提取",
			resp: &RespData{Choices: []Choice{{Message: Message{Content: "介绍"}}}},
			want: "介绍",
		},
		{
			name: "去掉首尾空白",
			resp: &RespData{Choices: []Choice{{Message: Message{Content: "  介绍\n\n"}}}},
			want: "介绍",
		},
		{
			name: "剥掉代码块围栏",
			resp: &RespData{Choices: []Choice{{Message: Message{Content: "```\n介绍\n```"}}}},
			want: "介绍",
		},
		{
			name: "剥掉带语言标注的围栏",
			resp: &RespData{Choices: []Choice{{Message: Message{Content: "```text\n介绍\n```"}}}},
			want: "介绍",
		},
		{
			name: "剥掉前缀标签",
			resp: &RespData{Choices: []Choice{{Message: Message{Content: "文献介绍：介绍"}}}},
			want: "介绍",
		},
		{
			name:    "没有 choices",
			resp:    &RespData{},
			wantErr: true,
		},
		{
			name:    "resp 为 nil",
			resp:    nil,
			wantErr: true,
		},
		{
			name:    "content 为空",
			resp:    &RespData{Choices: []Choice{{Message: Message{Content: "   "}}}},
			wantErr: true,
		},
		{
			// 被 max_tokens 截断的是半截话，进向量库只会污染检索结果。
			name: "被输出上限截断",
			resp: &RespData{Choices: []Choice{{
				Message:      Message{Content: "半截介绍"},
				FinishReason: "length",
			}}},
			wantErr: true,
		},
		{
			// 思考模式没关干净时内容会落在 reasoning_content 里，
			// 这种情况要显式报错（而不是把空串当成有效结果）。
			name: "内容落在 reasoning_content",
			resp: &RespData{Choices: []Choice{{Message: Message{
				Content:          "",
				ReasoningContent: "我在思考该如何概括这篇论文",
			}}}},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractSummary(tc.resp)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际返回 %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("extractSummary: %v", err)
			}
			if got != tc.want {
				t.Errorf("summary = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestGenerateSummaryRetriesThenSucceeds 生成失败会重试，前几次失败后成功仍返回结果。
func TestGenerateSummaryRetriesThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) <= 2 {
			http.Error(w, "upstream 500", http.StatusInternalServerError)
			return
		}
		writeOK(w, "重试后成功")
	}))
	defer srv.Close()

	s := NewSummarizer("test-key", testModel, WithEndpoint(srv.URL), testRetry())
	summary, err := s.GenerateSummary(context.Background(), "A Title", "An Abstract")
	if err != nil {
		t.Fatalf("GenerateSummary: %v", err)
	}
	if summary != "重试后成功" {
		t.Errorf("summary = %q, want 重试后成功", summary)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("请求 %d 次, want 3（失败 2 次后第 3 次成功）", got)
	}
}

// TestGenerateSummaryGivesUpAfterRetries 连续失败时最多请求 1+3 次，并把错误上报。
func TestGenerateSummaryGivesUpAfterRetries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "always down", http.StatusBadGateway)
	}))
	defer srv.Close()

	s := NewSummarizer("test-key", testModel, WithEndpoint(srv.URL), testRetry())
	if _, err := s.GenerateSummary(context.Background(), "A Title", "An Abstract"); err == nil {
		t.Fatal("持续失败时期望返回错误，实际返回 nil")
	}
	if got := atomic.LoadInt32(&calls); got != 4 {
		t.Errorf("请求 %d 次, want 4（首次 + 重试 3 次）", got)
	}
}

// TestGenerateSummaryRetriesOnEmptyResult 模型返回空内容同样算失败，会触发重试。
func TestGenerateSummaryRetriesOnEmptyResult(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		writeOK(w, "   ")
	}))
	defer srv.Close()

	s := NewSummarizer("test-key", testModel, WithEndpoint(srv.URL), testRetry())
	if _, err := s.GenerateSummary(context.Background(), "A Title", "An Abstract"); err == nil {
		t.Fatal("内容为空时期望返回错误，实际返回 nil")
	}
	if got := atomic.LoadInt32(&calls); got != 4 {
		t.Errorf("请求 %d 次, want 4（空内容也重试）", got)
	}
}

// TestGenerateSummaryRetriesOnTruncation 被 max_tokens 截断同样算失败，会触发重试。
func TestGenerateSummaryRetriesOnTruncation(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		writeContent(w, "半截介绍", "length")
	}))
	defer srv.Close()

	s := NewSummarizer("test-key", testModel, WithEndpoint(srv.URL), testRetry())
	if _, err := s.GenerateSummary(context.Background(), "A Title", "An Abstract"); err == nil {
		t.Fatal("被截断时期望返回错误，实际返回 nil")
	}
	if got := atomic.LoadInt32(&calls); got != 4 {
		t.Errorf("请求 %d 次, want 4（截断也重试）", got)
	}
}

// TestGenerateSummaryPermanentErrorNotRetried 4xx（如 key 无效）不重试，只请求一次。
func TestGenerateSummaryPermanentErrorNotRetried(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "Invalid API key", http.StatusUnauthorized)
	}))
	defer srv.Close()

	s := NewSummarizer("bad-key", testModel, WithEndpoint(srv.URL), testRetry())
	if _, err := s.GenerateSummary(context.Background(), "A Title", "An Abstract"); err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("请求 %d 次, want 1（4xx 属配置问题，不应重试）", got)
	}
}

// TestGenerateSummaryRetriesOnRateLimit 429 属于临时限流，仍应重试。
func TestGenerateSummaryRetriesOnRateLimit(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	s := NewSummarizer("test-key", testModel, WithEndpoint(srv.URL), testRetry())
	if _, err := s.GenerateSummary(context.Background(), "A Title", "An Abstract"); err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
	if got := atomic.LoadInt32(&calls); got != 4 {
		t.Errorf("请求 %d 次, want 4（429 应重试 3 次）", got)
	}
}

// TestGenerateSummaryHonorsSemaphore 批量生成文献介绍时，同时在飞的请求数不超过闸门容量。
// qwen3.8-flash 受 TPM（每分钟 5M）约束，并发失控会换来限流与退避重试，白白拖慢整批。
func TestGenerateSummaryHonorsSemaphore(t *testing.T) {
	const (
		limit   = 3
		workers = 12
	)

	var inFlight, peak atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := inFlight.Add(1)
		for {
			old := peak.Load()
			if cur <= old || peak.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		inFlight.Add(-1)

		writeOK(w, "介绍")
	}))
	defer srv.Close()

	s := NewSummarizer("test-key", testModel,
		WithEndpoint(srv.URL), testRetry(), WithSemaphore(semaphore.New(limit)))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.GenerateSummary(context.Background(), "T", "A"); err != nil {
				t.Errorf("GenerateSummary: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := peak.Load(); got > limit {
		t.Errorf("同时在飞的 summary 请求峰值 = %d, 超过闸门容量 %d", got, limit)
	}
	if got := peak.Load(); got < 2 {
		t.Errorf("峰值 = %d, 闸门可能把并发压成了串行", got)
	}
}

// TestNewSummarizerPassesOptions NewSummarizer 的 opts 要透传到 chat 客户端。
func TestNewSummarizerPassesOptions(t *testing.T) {
	gate := semaphore.New(2)
	s := NewSummarizer("k", "m", WithSemaphore(gate), testRetry())
	if s.client.gate != gate {
		t.Errorf("gate 未透传到 chat 客户端: %v", s.client.gate)
	}
}
