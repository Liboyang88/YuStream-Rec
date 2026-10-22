package translator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"searcher/retry"
)

// ---- provider 选择与默认值 ----

// TestNormalizeProvider provider 名称的归一：常见写法都接受，写错要能被发现。
func TestNormalizeProvider(t *testing.T) {
	cases := []struct {
		in   string
		want Provider
		ok   bool
	}{
		{"", ProviderAliyunMT, true}, // 留空默认 alimt
		{"alimt", ProviderAliyunMT, true},
		{"ALIMT", ProviderAliyunMT, true},
		{"aliyunmt", ProviderAliyunMT, true},
		{"aliyun-mt", ProviderAliyunMT, true},
		{"qwenmt", ProviderQwenMT, true},
		{"qwen-mt", ProviderQwenMT, true},
		{"QWEN_MT", ProviderQwenMT, true},
		{"qwen", ProviderQwenMT, true},
		{"dashscope", ProviderQwenMT, true},
		{"  qwenmt  ", ProviderQwenMT, true},
		{"google", "", false},
		{"qwen-mt-lite", "", false}, // 模型名不是 provider
	}
	for _, tc := range cases {
		got, ok := NormalizeProvider(tc.in)
		if ok != tc.ok {
			t.Errorf("NormalizeProvider(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("NormalizeProvider(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestNewSelectsProvider 不同 provider 返回不同实现，且各要各的凭据。
func TestNewSelectsProvider(t *testing.T) {
	alimt, err := New(Config{Provider: ProviderAliyunMT, AccessKeyID: "id", AccessKeySecret: "secret"})
	if err != nil {
		t.Fatalf("alimt: %v", err)
	}
	if _, ok := alimt.(*alimtClient); !ok {
		t.Errorf("Provider=alimt 应返回 *alimtClient，实际 %T", alimt)
	}

	qwen, err := New(Config{Provider: ProviderQwenMT, APIKey: "sk-x"})
	if err != nil {
		t.Fatalf("qwenmt: %v", err)
	}
	if _, ok := qwen.(*qwenClient); !ok {
		t.Errorf("Provider=qwenmt 应返回 *qwenClient，实际 %T", qwen)
	}
}

// TestNewCredentialErrors 选中的 provider 缺凭据要直接报错（而不是静默降级）。
func TestNewCredentialErrors(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"alimt 缺 AccessKey", Config{Provider: ProviderAliyunMT}, "AccessKeyID"},
		{"alimt 只有 ID", Config{Provider: ProviderAliyunMT, AccessKeyID: "id"}, "AccessKeySecret"},
		{"qwenmt 缺 ApiKey", Config{Provider: ProviderQwenMT}, "APIKey"},
		{"provider 非法", Config{Provider: "google"}, "未知的翻译 provider"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.cfg)
			if err == nil {
				t.Fatal("期望报错，实际 nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("错误信息应提到 %q，实际: %v", tc.want, err)
			}
		})
	}
}

// TestProviderDefaultLimits 两种 provider 的默认限速必须分开：
// alimt 走高 QPS，qwenmt 必须是 1 RPS + 低并发。
func TestProviderDefaultLimits(t *testing.T) {
	alimt, err := New(Config{Provider: ProviderAliyunMT, AccessKeyID: "id", AccessKeySecret: "s"})
	if err != nil {
		t.Fatalf("alimt: %v", err)
	}
	ac := alimt.(*alimtClient)
	if got := ac.limiter.Rate(); got != DefaultAlimtQPS {
		t.Errorf("alimt 默认 QPS = %v, want %v", got, DefaultAlimtQPS)
	}
	if got := ac.gate.Cap(); got != DefaultAlimtConcurrency {
		t.Errorf("alimt 默认并发 = %d, want %d", got, DefaultAlimtConcurrency)
	}

	qwen, err := New(Config{Provider: ProviderQwenMT, APIKey: "sk-x"})
	if err != nil {
		t.Fatalf("qwenmt: %v", err)
	}
	qc := qwen.(*qwenClient)
	if qc.limiter == nil {
		t.Fatal("qwenmt 必须有默认限速器（只有 60 RPM）")
	}
	if got := qc.limiter.Rate(); got != DefaultQwenQPS {
		t.Errorf("qwenmt 默认 QPS = %v, want %v", got, DefaultQwenQPS)
	}
	if got := qc.limiter.Burst(); got != DefaultQwenBurst {
		t.Errorf("qwenmt 默认 burst = %d, want %d（不突发）", got, DefaultQwenBurst)
	}
	if got := qc.gate.Cap(); got != DefaultQwenConcurrency {
		t.Errorf("qwenmt 默认并发 = %d, want %d", got, DefaultQwenConcurrency)
	}
	// 默认档位必须比 alimt 保守得多，否则会不断触发百炼的突发保护。
	if qc.limiter.Rate() >= ac.limiter.Rate() {
		t.Errorf("qwenmt(%v) 的默认速率不该 >= alimt(%v)", qc.limiter.Rate(), ac.limiter.Rate())
	}
}

// TestResolveLimitsExplicitOverride 显式配置要覆盖 provider 默认值。
func TestResolveLimitsExplicitOverride(t *testing.T) {
	qwen, err := New(Config{
		Provider: ProviderQwenMT, APIKey: "sk-x",
		QPS: 5, Burst: 3, Concurrency: 7,
	})
	if err != nil {
		t.Fatalf("qwenmt: %v", err)
	}
	qc := qwen.(*qwenClient)
	if got := qc.limiter.Rate(); got != 5 {
		t.Errorf("QPS = %v, want 5", got)
	}
	if got := qc.limiter.Burst(); got != 3 {
		t.Errorf("burst = %d, want 3", got)
	}
	if got := qc.gate.Cap(); got != 7 {
		t.Errorf("并发 = %d, want 7", got)
	}

	// 负值 = 不限
	unlimited, err := New(Config{
		Provider: ProviderQwenMT, APIKey: "sk-x",
		QPS: -1, Concurrency: -1,
	})
	if err != nil {
		t.Fatalf("qwenmt: %v", err)
	}
	uc := unlimited.(*qwenClient)
	if uc.limiter != nil {
		t.Errorf("QPS=-1 应关闭限速，实际 %v", uc.limiter)
	}
	if uc.gate != nil {
		t.Errorf("Concurrency=-1 应关闭并发闸门，实际 %v", uc.gate)
	}
}

// ---- Qwen-MT 协议 ----

// TestQwenRequestShape 校验 Qwen-MT 的请求形态：Bearer 鉴权、JSON body、
// OpenAI 兼容的 messages、以及 translation_options 里的全称语言码。
func TestQwenRequestShape(t *testing.T) {
	var (
		gotAuth string
		gotReq  qwenRequest
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, err.Error(), 400)
			return
		}
		writeQwenContent(w, "译文")
	}))
	defer srv.Close()

	c := newQwenTestClient(t, srv.URL, nil)
	if _, err := c.Translate(context.Background(), "hello", LangAuto, LangChinese); err != nil {
		t.Fatalf("Translate: %v", err)
	}

	if gotAuth != "Bearer "+testQwenAPIKey {
		t.Errorf("Authorization = %q, want Bearer %s", gotAuth, testQwenAPIKey)
	}
	if gotReq.Model == "" {
		t.Error("请求缺少 model")
	}
	if len(gotReq.Messages) != 1 || gotReq.Messages[0].Role != "user" {
		t.Fatalf("messages = %+v, want 单条 user 消息", gotReq.Messages)
	}
	if gotReq.Messages[0].Content != "hello" {
		t.Errorf("待翻译文本 = %q, want hello", gotReq.Messages[0].Content)
	}
	// Qwen-MT 用全称语言码，与 alimt 的 zh/en 不同。
	if gotReq.TranslationOptions.SourceLang != "auto" {
		t.Errorf("source_lang = %q, want auto", gotReq.TranslationOptions.SourceLang)
	}
	if gotReq.TranslationOptions.TargetLang != "Chinese" {
		t.Errorf("target_lang = %q, want Chinese（Qwen-MT 用全称）", gotReq.TranslationOptions.TargetLang)
	}
	// 术语表由客户端占位符方案处理，不再下发原生 glossary，避免两套机制互相干扰。
	if len(gotReq.TranslationOptions.Glossary) != 0 {
		t.Errorf("不该下发原生 glossary，实际 %+v", gotReq.TranslationOptions.Glossary)
	}
}

// TestQwenDefaultModel 未指定模型时用 qwen-mt-lite。
func TestQwenDefaultModel(t *testing.T) {
	var gotReq qwenRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotReq)
		writeQwenContent(w, "译文")
	}))
	defer srv.Close()

	c := newQwenTestClient(t, srv.URL, nil)
	if _, err := c.Translate(context.Background(), "hi", LangAuto, LangChinese); err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if gotReq.Model != qwenDefaultModel {
		t.Errorf("model = %q, want %q", gotReq.Model, qwenDefaultModel)
	}
}

// TestQwenCustomModel 显式指定模型时按指定值下发。
func TestQwenCustomModel(t *testing.T) {
	var gotReq qwenRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotReq)
		writeQwenContent(w, "译文")
	}))
	defer srv.Close()

	c := newQwenTestClient(t, srv.URL, func(cfg *Config) { cfg.Model = "qwen-mt-plus" })
	if _, err := c.Translate(context.Background(), "hi", LangAuto, LangChinese); err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if gotReq.Model != "qwen-mt-plus" {
		t.Errorf("model = %q, want qwen-mt-plus", gotReq.Model)
	}
}

// TestQwenNormalizeLang 全称语言码的转换。
func TestQwenNormalizeLang(t *testing.T) {
	cases := []struct{ in, want string }{
		{"auto", "auto"},
		{LangAuto, "auto"},
		{"", "auto"},
		{"Chinese", "Chinese"},
		{"zh", "Chinese"},
		{"cn", "Chinese"},
		{"中文", "Chinese"},
		{"English", "English"},
		{"en", "English"},
		{"ja", "ja"},
	}
	for _, tc := range cases {
		if got := normalizeQwenLang(tc.in); got != tc.want {
			t.Errorf("normalizeQwenLang(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestQwenTranslateItemMerges 标题与摘要合并为一次请求（与 alimt 行为一致）。
func TestQwenTranslateItemMerges(t *testing.T) {
	var calls int32
	var gotText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		var req qwenRequest
		json.NewDecoder(r.Body).Decode(&req)
		gotText = req.Messages[0].Content

		parts := strings.Split(gotText, pairSeparator)
		writeQwenContent(w, "译:"+parts[0]+pairSeparator+"译:"+parts[1])
	}))
	defer srv.Close()

	c := newQwenTestClient(t, srv.URL, nil)
	titleZh, abstractZh, err := c.TranslateItem(context.Background(), "A Title", "An Abstract")
	if err != nil {
		t.Fatalf("TranslateItem: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("请求 %d 次, want 1（标题与摘要合并）", got)
	}
	if gotText != "A Title"+pairSeparator+"An Abstract" {
		t.Errorf("请求文本 = %q", gotText)
	}
	if titleZh != "译:A Title" || abstractZh != "译:An Abstract" {
		t.Errorf("翻译结果 = (%q, %q)", titleZh, abstractZh)
	}
}

// TestQwenDoesNotChunk Qwen-MT 没有 5000 字符上限，不该被分段：
// 超长文本必须仍是一次请求，否则会白白多花调用（额度只有 1 RPS）。
func TestQwenDoesNotChunk(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		writeQwenContent(w, "译文")
	}))
	defer srv.Close()

	long := strings.Repeat("This is a long sentence. ", 400) // 约 10000 字符

	c := newQwenTestClient(t, srv.URL, nil)
	if _, err := c.Translate(context.Background(), long, LangAuto, LangChinese); err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("请求 %d 次, want 1（Qwen-MT 不该分段）", got)
	}
}

// TestQwenAPIError 响应里的 error 字段要作为错误抛出。
func TestQwenAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"code": "InvalidApiKey", "message": "bad key"},
		})
	}))
	defer srv.Close()

	c := newQwenTestClient(t, srv.URL, nil)
	if _, err := c.Translate(context.Background(), "hi", LangAuto, LangChinese); err == nil {
		t.Fatal("期望返回错误，实际 nil")
	}
}

// TestQwenEmptyChoices 没有 choices 时报错，而不是返回空串。
func TestQwenEmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	c := newQwenTestClient(t, srv.URL, nil)
	if _, err := c.Translate(context.Background(), "hi", LangAuto, LangChinese); err == nil {
		t.Fatal("空 choices 期望报错，实际 nil")
	}
}

// TestQwenPermanentErrorNotRetried 4xx（key 无效）不重试，只请求一次。
func TestQwenPermanentErrorNotRetried(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "InvalidApiKey", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newQwenTestClient(t, srv.URL, nil)
	if _, err := c.Translate(context.Background(), "hi", LangAuto, LangChinese); err == nil {
		t.Fatal("期望返回错误，实际 nil")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("请求 %d 次, want 1（4xx 不重试）", got)
	}
}

// TestQwenRetriesOnRateLimit 429 属于临时限流，应当重试。
func TestQwenRetriesOnRateLimit(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			http.Error(w, "Throttling", http.StatusTooManyRequests)
			return
		}
		writeQwenContent(w, "译文")
	}))
	defer srv.Close()

	c := newQwenTestClient(t, srv.URL, nil)
	if _, err := c.Translate(context.Background(), "hi", LangAuto, LangChinese); err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("请求 %d 次, want 2（429 后重试成功）", got)
	}
}

// TestQwenGlossary 术语表在 Qwen-MT 上同样生效（客户端占位符方案）。
func TestQwenGlossary(t *testing.T) {
	var gotText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req qwenRequest
		json.NewDecoder(r.Body).Decode(&req)
		gotText = req.Messages[0].Content
		writeQwenContent(w, "译文 "+gotText)
	}))
	defer srv.Close()

	c := newQwenTestClient(t, srv.URL, withGlossary([]GlossaryItem{{Term: "RAG", Translation: "检索增强生成"}}))
	out, err := c.Translate(context.Background(), "RAG works", LangAuto, LangChinese)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if strings.Contains(gotText, "RAG") {
		t.Errorf("请求文本仍含原术语（未被占位符替换）: %q", gotText)
	}
	if !strings.Contains(out, "检索增强生成") {
		t.Errorf("译文应含术语的固定译法，实际 %q", out)
	}
}

// TestQwenRateLimitIsEnforced Qwen-MT 的核心约束：只靠并发数挡不住"连发"，
// 必须按速率放行。这里用默认的 1 RPS + 不突发验证：
// 连续 4 次翻译至少要 3 秒（与换 alimt 之前的 qwen-mt-lite 口径一致）。
func TestQwenRateLimitIsEnforced(t *testing.T) {
	// 这两条限速用例靠真实时钟等待（各约 8s / 5s），彼此独立，并行跑以缩短整包时间。
	t.Parallel()

	var arrivals []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrivals = append(arrivals, time.Now())
		writeQwenContent(w, "译文")
	}))
	defer srv.Close()

	// 用默认限流（QPS=1, burst=1），不显式覆盖——这正是要保证的行为。
	c := newQwenTestClient(t, srv.URL, nil)

	const calls = 4
	start := time.Now()
	for i := 0; i < calls; i++ {
		if _, err := c.Translate(context.Background(), "hello", LangAuto, LangChinese); err != nil {
			t.Fatalf("第 %d 次 Translate: %v", i+1, err)
		}
	}
	elapsed := time.Since(start)

	// 第 1 次立即发出，之后每次隔 1s：共 (calls-1) 秒。
	if elapsed < time.Duration(calls-1)*time.Second-150*time.Millisecond {
		t.Errorf("%d 次翻译耗时 %v, want 至少 %v（默认每秒 1 个请求）",
			calls, elapsed, time.Duration(calls-1)*time.Second)
	}
	if elapsed > time.Duration(calls)*time.Second+time.Second {
		t.Errorf("%d 次翻译耗时 %v, 明显超过预期，排队算多了（白等）", calls, elapsed)
	}
	t.Logf("%d 次翻译耗时 %v（期望约 3s）", calls, elapsed)
}

// TestQwenConcurrentRequestsArePaced 并发调用时按速率放行：
// 这是 Qwen-MT 相对 alimt 的关键区别——并发闸门管不住"发出的节奏"。
func TestQwenConcurrentRequestsArePaced(t *testing.T) {
	t.Parallel()

	var arrivals []time.Time
	var mu = make(chan struct{}, 1)
	mu <- struct{}{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-mu
		arrivals = append(arrivals, time.Now())
		mu <- struct{}{}
		writeQwenContent(w, "译文")
	}))
	defer srv.Close()

	// QPS 4、burst 1：6 个并发请求的整体跨度至少约 1.25s
	//（没有限速器时它们会在几毫秒内全部发出）。
	c := newQwenTestClient(t, srv.URL, func(cfg *Config) { cfg.QPS, cfg.Burst = 4, 1 })

	const workers = 6
	done := make(chan struct{})
	for i := 0; i < workers; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			if _, err := c.Translate(context.Background(), "hello", LangAuto, LangChinese); err != nil {
				t.Errorf("Translate: %v", err)
			}
		}()
	}
	for i := 0; i < workers; i++ {
		<-done
	}

	<-mu
	got := arrivals
	mu <- struct{}{}

	if len(got) != workers {
		t.Fatalf("请求数 = %d, want %d", len(got), workers)
	}
	span := got[len(got)-1].Sub(got[0])
	if span < 900*time.Millisecond {
		t.Errorf("%d 个并发请求的总跨度 = %v, 期望至少 900ms（QPS 4 时限速器应把它们排开）", workers, span)
	}
	t.Logf("跨度 = %v（理论约 1.25s）", span)
}

// TestQwenRetryConfigIsHonored 重试策略透传到 Qwen-MT 客户端。
func TestQwenRetryConfigIsHonored(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()

	cfg := Config{
		Provider: ProviderQwenMT, APIKey: testQwenAPIKey,
		Endpoint: srv.URL,
		Retry:    &retry.Config{MaxRetries: 1, Backoff: 0},
	}
	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client.Translate(context.Background(), "hi", LangAuto, LangChinese)

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("请求 %d 次, want 2（首次 + 重试 1 次）", got)
	}
}

// writeQwenContent 返回一段 Qwen-MT 的成功响应。
func writeQwenContent(w http.ResponseWriter, content string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"choices": []map[string]any{
			{"message": map[string]string{"role": "assistant", "content": content}},
		},
	})
}
