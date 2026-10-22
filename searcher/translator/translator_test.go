package translator

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"searcher/retry"
)

// 测试用的凭据：签名可被服务端复算，因此不必是真实 AccessKey。
const (
	testAccessKeyID     = "test-access-key-id"
	testAccessKeySecret = "test-access-key-secret"
	testQwenAPIKey      = "sk-test-qwen-api-key"
)

// testRetry 使用零退避的重试策略，避免失败用例真的等待 2s/4s/6s。
func testRetry() retry.Config {
	return retry.Config{MaxRetries: 3, Backoff: 0}
}

// testAlimtConfig 返回一份指向测试服务器的 alimt 配置（零退避重试、不限流）。
func testAlimtConfig(endpoint string) Config {
	cfg := Config{
		Provider:        ProviderAliyunMT,
		AccessKeyID:     testAccessKeyID,
		AccessKeySecret: testAccessKeySecret,
		Endpoint:        endpoint,
		Retry:           ptrRetry(testRetry()),
	}
	return cfg
}

func ptrRetry(r retry.Config) *retry.Config { return &r }

// withGlossary 覆盖术语表。
func withGlossary(g []GlossaryItem) func(*Config) {
	return func(cfg *Config) { cfg.Glossary = g }
}

// newTestClient 构造指向测试服务器的 alimt 客户端（零退避重试、默认限流）。
func newTestClient(t *testing.T, endpoint string, op func(*Config)) *alimtClient {
	t.Helper()
	cfg := testAlimtConfig(endpoint)
	if op != nil {
		op(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("构造 alimt 客户端: %v", err)
	}
	ac, ok := c.(*alimtClient)
	if !ok {
		t.Fatalf("Provider=alimt 应返回 *alimtClient，实际 %T", c)
	}
	return ac
}

// newQwenTestClient 构造指向测试服务器的 Qwen-MT 客户端（零退避重试）。
func newQwenTestClient(t *testing.T, endpoint string, op func(*Config)) *qwenClient {
	t.Helper()
	cfg := Config{
		Provider: ProviderQwenMT,
		APIKey:   testQwenAPIKey,
		Endpoint: endpoint,
		Retry:    ptrRetry(testRetry()),
	}
	if op != nil {
		op(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("构造 qwen 客户端: %v", err)
	}
	qc, ok := c.(*qwenClient)
	if !ok {
		t.Fatalf("Provider=qwenmt 应返回 *qwenClient，实际 %T", c)
	}
	return qc
}

// translateClient 是测试里统一使用的接口类型。
type translateClient = Client

// signHere 用与服务端独立的实现复算签名，用于验证客户端签名逻辑。
func signHere(params url.Values, secret string) string {
	canonical := canonicalQuery(params)
	stringToSign := httpMethod + "&" + percentEncode("/") + "&" + percentEncode(canonical)
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// readForm 解析请求参数，并校验签名与必填项（模拟网关的检查）。
// 返回解析后的表单；校验失败时已经写过响应，调用方直接 return 即可。
func readForm(t *testing.T, w http.ResponseWriter, r *http.Request) url.Values {
	t.Helper()

	if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/x-www-form-urlencoded") {
		t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", got)
	}
	if err := r.ParseForm(); err != nil {
		t.Errorf("ParseForm: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil
	}

	params := r.PostForm
	signature := params.Get("Signature")
	if signature == "" {
		t.Error("请求缺少 Signature 参数")
	} else if want := signHere(params, testAccessKeySecret); signature != want {
		t.Errorf("签名不匹配\n got = %s\nwant = %s\n规范化串 = %s",
			signature, want, canonicalQuery(params))
	}

	for _, key := range []string{"AccessKeyId", "SignatureNonce", "Timestamp", "Action", "Version", "SourceText"} {
		if params.Get(key) == "" {
			t.Errorf("请求缺少参数 %s", key)
		}
	}
	if got := params.Get("Action"); got != apiAction {
		t.Errorf("Action = %q, want %q", got, apiAction)
	}
	if got := params.Get("Version"); got != apiVersion {
		t.Errorf("Version = %q, want %q", got, apiVersion)
	}
	return params
}

// writeTranslated 返回一次成功的翻译响应（接口字段为大驼峰）。
func writeTranslated(w http.ResponseWriter, translated string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"Code":      successCode,
		"Message":   "success",
		"RequestId": "test-request-id",
		"Data": map[string]any{
			"Translated": translated,
			"WordCount":  "2",
		},
	})
}

// writeAPIError 返回接口约定的业务错误（HTTP 200 + 非 200 的 Code）。
func writeAPIError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"Code":      code,
		"Message":   message,
		"RequestId": "test-request-id",
	})
}

// TestSplitPairTolerant 机器翻译会在符号周围插空格，分隔符回来时可能变形。
//
// 实测 TranslateGeneral 对同一个分隔符会给出两种形态，都只是排版差异：
//
//	||| SPLIT_MT |||    （每个 | 后插空格，最常见）
//	| SPLIT_MT | |       （开头的 ||| 被并成一个 |）
//
// 这两种必须都能拆开，否则会白白走一遍"标题与摘要分别翻译"的降级（多花一倍调用）。
func TestSplitPairTolerant(t *testing.T) {
	cases := []struct {
		name         string
		translated   string
		wantTitle    string
		wantAbstract string
		wantErr      bool
	}{
		{"原样", "标题" + pairSeparator + "摘要", "标题", "摘要", false},
		{"每个竖线后插空格", "标题 ||| SPLIT_MT ||| 摘要", "标题", "摘要", false},
		{"开头竖线被合并", "标题 | SPLIT_MT | | 摘要", "标题", "摘要", false},
		{"只有一侧有空格", "标题||| SPLIT_MT|||摘要", "标题", "摘要", false},
		{"大小写被改", "标题 ||| split_mt ||| 摘要", "标题", "摘要", false},
		{"完全变形（真该降级）", "标题和摘要被合并成了一整段", "", "", true},
		{"分隔符出现两次", "a" + pairSeparator + "b" + pairSeparator + "c", "", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			title, abstract, err := splitPair(tc.translated, pairSeparator)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际返回 (%q, %q)", title, abstract)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitPair: %v", err)
			}
			if title != tc.wantTitle || abstract != tc.wantAbstract {
				t.Errorf("拆分结果 = (%q, %q), want (%q, %q)", title, abstract, tc.wantTitle, tc.wantAbstract)
			}
		})
	}
}

// TestTranslateItemSpacedSeparatorNoFallback 接口把分隔符加了空格时不该触发降级重译——
// 那会让每次翻译都多花一倍调用。
func TestTranslateItemSpacedSeparatorNoFallback(t *testing.T) {
	var combined, fallback int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := readForm(t, w, r)
		if params == nil {
			return
		}
		text := params.Get("SourceText")
		if strings.Contains(text, pairSeparator) {
			atomic.AddInt32(&combined, 1)
			// 模拟真实接口：在每个 | 后插空格
			writeTranslated(w, "译:"+strings.ReplaceAll(text, "|", "| "))
			return
		}
		atomic.AddInt32(&fallback, 1)
		writeTranslated(w, "降级译文")
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	titleZh, abstractZh, err := c.TranslateItem(context.Background(), "A Title", "An Abstract")
	if err != nil {
		t.Fatalf("TranslateItem: %v", err)
	}
	if got := atomic.LoadInt32(&fallback); got != 0 {
		t.Errorf("降级请求 %d 次, want 0（分隔符只是多了空格，不该触发降级）", got)
	}
	if got := atomic.LoadInt32(&combined); got != 1 {
		t.Errorf("合并翻译请求 %d 次, want 1", got)
	}
	if !strings.Contains(titleZh, "A Title") || !strings.Contains(abstractZh, "An Abstract") {
		t.Errorf("拆分结果 = (%q, %q)，应分别含标题与摘要", titleZh, abstractZh)
	}
}

// TestCodeValueUnmarshal 接口的 Code 字段实测是字符串（"200"），文档写的是数字。
// 两种形态都要能解析，否则一次成功的翻译会被误判成失败并重试 4 次。
func TestCodeValueUnmarshal(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"字符串 200", `{"Code":"200"}`, 200},
		{"数字 200", `{"Code":200}`, 200},
		{"字符串错误码", `{"Code":"10005"}`, 10005},
		{"数字错误码", `{"Code":10005}`, 10005},
		{"空串", `{"Code":""}`, 0},
		{"null", `{"Code":null}`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var resp alimtResponse
			if err := json.Unmarshal([]byte(tc.body), &resp); err != nil {
				t.Fatalf("Unmarshal(%s): %v", tc.body, err)
			}
			if int(resp.Code) != tc.want {
				t.Errorf("Code = %d, want %d", int(resp.Code), tc.want)
			}
		})
	}
}

// TestParseStringCodeResponse 整条响应用字符串 Code 时也要能正常走通（真实接口形态）。
func TestParseStringCodeResponse(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		// 真实接口返回的字段与类型
		w.Write([]byte(`{"RequestId":"x","Data":{"WordCount":"2","Translated":"你好","DetectedLanguage":"en"},"Code":"200"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	out, err := c.Translate(context.Background(), "hello", LangAuto, LangChinese)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if out != "你好" {
		t.Errorf("译文 = %q, want 你好", out)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("请求 %d 次, want 1（字符串 Code 不该导致重试）", got)
	}
}

// TestPermanentErrorCodeAsString 字符串形态的错误码同样要按"不可重试"处理。
func TestPermanentErrorCodeAsString(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"RequestId":"x","Code":"10010","Message":"账号没有开通服务"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	if _, err := c.Translate(context.Background(), "hello", LangAuto, LangChinese); err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("请求 %d 次, want 1（字符串 10010 不应重试）", got)
	}
}

// TestSplitPair 拆分逻辑与接口无关，保持不变。
func TestSplitPair(t *testing.T) {
	cases := []struct {
		name         string
		translated   string
		wantTitle    string
		wantAbstract string
		wantErr      bool
	}{
		{"正常拆分", "标题译文" + pairSeparator + "摘要译文", "标题译文", "摘要译文", false},
		{"带空白", "  标题译文  " + pairSeparator + "\n摘要译文\t", "标题译文", "摘要译文", false},
		{"缺少分隔符", "只有一段文本", "", "", true},
		{"分隔符过多", "a" + pairSeparator + "b" + pairSeparator + "c", "", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			title, abstract, err := splitPair(tc.translated, pairSeparator)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际返回 (%q, %q)", title, abstract)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitPair: %v", err)
			}
			if title != tc.wantTitle || abstract != tc.wantAbstract {
				t.Errorf("拆分结果 = (%q, %q), want (%q, %q)", title, abstract, tc.wantTitle, tc.wantAbstract)
			}
		})
	}
}

// TestRequestShapeAndSignature 校验请求形态：表单编码、公共参数、业务参数，
// 以及 HMAC-SHA1 签名能被独立复算出来（签名错了真实接口会直接拒绝）。
func TestRequestShapeAndSignature(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		got = readForm(t, w, r)
		if got == nil {
			return
		}
		writeTranslated(w, "你好")
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	out, err := c.Translate(context.Background(), "hello", LangAuto, LangChinese)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if out != "你好" {
		t.Errorf("译文 = %q, want 你好", out)
	}

	if v := got.Get("SourceText"); v != "hello" {
		t.Errorf("SourceText = %q, want hello", v)
	}
	if v := got.Get("SourceLanguage"); v != "auto" {
		t.Errorf("SourceLanguage = %q, want auto", v)
	}
	if v := got.Get("TargetLanguage"); v != "zh" {
		t.Errorf("TargetLanguage = %q, want zh（alimt 用简短语言码）", v)
	}
	if v := got.Get("FormatType"); v != "text" {
		t.Errorf("FormatType = %q, want text", v)
	}
	if v := got.Get("Scene"); v != defaultScene {
		t.Errorf("Scene = %q, want %q", v, defaultScene)
	}
	if v := got.Get("Format"); v != "JSON" {
		t.Errorf("Format = %q, want JSON", v)
	}
	if v := got.Get("SignatureMethod"); v != signatureMethod {
		t.Errorf("SignatureMethod = %q, want %q", v, signatureMethod)
	}
	if v := got.Get("AccessKeyId"); v != testAccessKeyID {
		t.Errorf("AccessKeyId = %q, want %q", v, testAccessKeyID)
	}
	if _, err := time.Parse("2006-01-02T15:04:05Z", got.Get("Timestamp")); err != nil {
		t.Errorf("Timestamp = %q 不是接口要求的 UTC 格式: %v", got.Get("Timestamp"), err)
	}
}

// TestSignatureNonceUnique SignatureNonce 必须每次不同，否则会被判定为重放。
func TestSignatureNonceUnique(t *testing.T) {
	nonces := map[string]bool{}
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := readForm(t, w, r)
		if params == nil {
			return
		}
		mu.Lock()
		nonce := params.Get("SignatureNonce")
		if nonces[nonce] {
			t.Errorf("SignatureNonce 重复: %q", nonce)
		}
		nonces[nonce] = true
		mu.Unlock()
		writeTranslated(w, "译文")
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	for i := 0; i < 5; i++ {
		if _, err := c.Translate(context.Background(), "hello", LangAuto, LangChinese); err != nil {
			t.Fatalf("第 %d 次 Translate: %v", i+1, err)
		}
	}
}

// TestNormalizeLang 语言标识统一成 alimt 的语言码。
func TestNormalizeLang(t *testing.T) {
	cases := []struct{ in, want string }{
		{"auto", "auto"},
		{"Auto", "auto"},
		{"", "auto"},
		{"Chinese", "zh"},
		{"zh", "zh"},
		{"CN", "zh"},
		{"中文", "zh"},
		{"English", "en"},
		{"en", "en"},
		{"  en  ", "en"},
		{"ja", "ja"}, // 其他语言码原样透传
	}
	for _, tc := range cases {
		if got := normalizeLang(tc.in); got != tc.want {
			t.Errorf("normalizeLang(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestTranslateItemMergesTitleAndAbstract 标题与摘要合并为一次请求，再按分隔符拆回。
func TestTranslateItemMergesTitleAndAbstract(t *testing.T) {
	var (
		calls     int
		gotText   string
		gotSource string
		gotTarget string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		params := readForm(t, w, r)
		if params == nil {
			return
		}
		gotText = params.Get("SourceText")
		gotSource = params.Get("SourceLanguage")
		gotTarget = params.Get("TargetLanguage")

		// 模拟"翻译"：原样返回并替换为中文占位。
		parts := strings.Split(gotText, pairSeparator)
		writeTranslated(w, "译:"+parts[0]+pairSeparator+"译:"+parts[1])
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	titleZh, abstractZh, err := c.TranslateItem(context.Background(), "A Title", "An Abstract")
	if err != nil {
		t.Fatalf("TranslateItem: %v", err)
	}
	if calls != 1 {
		t.Errorf("请求次数 = %d, want 1（标题与摘要合并为一次请求）", calls)
	}
	if gotText != "A Title"+pairSeparator+"An Abstract" {
		t.Errorf("请求文本 = %q", gotText)
	}
	if gotSource != "auto" || gotTarget != "zh" {
		t.Errorf("语言参数 = (%q, %q), want (auto, zh)", gotSource, gotTarget)
	}
	if titleZh != "译:A Title" || abstractZh != "译:An Abstract" {
		t.Errorf("翻译结果 = (%q, %q)", titleZh, abstractZh)
	}
}

// TestTranslateItemEmptyAbstract 没有摘要时只翻译标题，不发送多余的分隔符。
func TestTranslateItemEmptyAbstract(t *testing.T) {
	var gotText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := readForm(t, w, r)
		if params == nil {
			return
		}
		gotText = params.Get("SourceText")
		writeTranslated(w, "标题译文")
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	titleZh, abstractZh, err := c.TranslateItem(context.Background(), "A Title", "   ")
	if err != nil {
		t.Fatalf("TranslateItem: %v", err)
	}
	if gotText != "A Title" {
		t.Errorf("请求文本 = %q, want 仅标题", gotText)
	}
	if strings.Contains(gotText, pairSeparator) {
		t.Errorf("标题单独翻译时不该带分隔符: %q", gotText)
	}
	if titleZh != "标题译文" || abstractZh != "" {
		t.Errorf("翻译结果 = (%q, %q), want (标题译文, \"\")", titleZh, abstractZh)
	}
}

// TestTranslateHTTPErrorStatus 非 2xx（例如网关返回 HTML）时应带上状态码报错，
// 而不是把 HTML 当 JSON 解析后报"解析失败"。
func TestTranslateHTTPErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "<html>Bad Gateway</html>", http.StatusBadGateway)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	_, err := c.Translate(context.Background(), "hi", LangAuto, LangChinese)
	if err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("错误信息应包含状态码 502，实际: %v", err)
	}
}

// TestTranslateAPIError 接口以 HTTP 200 + 非 200 的 Code 报错时也要当成错误。
func TestTranslateAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAPIError(w, 10007, "An error occurred while translating.")
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	_, err := c.Translate(context.Background(), "hi", LangAuto, LangChinese)
	if err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
	if !strings.Contains(err.Error(), "10007") {
		t.Errorf("错误信息应包含错误码 10007，实际: %v", err)
	}
}

// TestTranslatePermanentAPICodeNotRetried 权限/语种这类错误码重试无意义，只请求一次。
func TestTranslatePermanentAPICodeNotRetried(t *testing.T) {
	cases := []struct {
		name string
		code int
	}{
		{"语种不支持", 10005},
		{"无权限", 10009},
		{"未开通服务", 10010},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				writeAPIError(w, tc.code, "permanent")
			}))
			defer srv.Close()

			c := newTestClient(t, srv.URL, nil)
			if _, err := c.Translate(context.Background(), "hi", LangAuto, LangChinese); err == nil {
				t.Fatal("期望返回错误，实际返回 nil")
			}
			if got := atomic.LoadInt32(&calls); got != 1 {
				t.Errorf("请求 %d 次, want 1（错误码 %d 不应重试）", got, tc.code)
			}
		})
	}
}

// TestTranslateRetriesOnTransientAPICode 系统错误/超时这类错误码属于临时状况，应当重试。
func TestTranslateRetriesOnTransientAPICode(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) <= 2 {
			writeAPIError(w, 10002, "A system error occurred.")
			return
		}
		writeTranslated(w, "译文")
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	out, err := c.Translate(context.Background(), "hello", LangAuto, LangChinese)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if out != "译文" {
		t.Errorf("译文 = %q, want 译文", out)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("请求 %d 次, want 3（失败 2 次后第 3 次成功）", got)
	}
}

// TestTranslateRetriesOnRateLimitLimit HTTP 429 属于临时限流，应当重试。
func TestTranslateRetriesOnRateLimitLimit(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			http.Error(w, "Throttling.User", http.StatusTooManyRequests)
			return
		}
		writeTranslated(w, "译文")
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	if _, err := c.Translate(context.Background(), "hello", LangAuto, LangChinese); err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("请求 %d 次, want 2（429 后重试成功）", got)
	}
}

// TestTranslateEmptyResultIsError 接口返回空译文时视为失败（不能让空串进中文字段）。
func TestTranslateEmptyResultIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTranslated(w, "   ")
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	if _, err := c.Translate(context.Background(), "hello", LangAuto, LangChinese); err == nil {
		t.Fatal("空译文期望返回错误，实际返回 nil")
	}
}

// TestTranslateGivesUpAfterRetries 连续失败时最多请求 1+3 次。
func TestTranslateGivesUpAfterRetries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "always down", http.StatusBadGateway)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	if _, err := c.Translate(context.Background(), "hello", LangAuto, LangChinese); err == nil {
		t.Fatal("持续失败时期望返回错误，实际返回 nil")
	}
	if got := atomic.LoadInt32(&calls); got != 4 {
		t.Errorf("请求 %d 次, want 4（首次 + 重试 3 次）", got)
	}
}

// TestTranslatePermanentErrorNotRetried 4xx（如签名/权限问题）不重试，只请求一次。
func TestTranslatePermanentErrorNotRetried(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "InvalidAccessKeyId", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	if _, err := c.Translate(context.Background(), "hello", LangAuto, LangChinese); err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("请求 %d 次, want 1（4xx 不应重试）", got)
	}
}

// TestTranslateItemFallsBackWhenSplitFails 合并翻译的结果无法按分隔符拆分时，
// 降级为标题与摘要分别翻译，最终仍拿到两段译文。
func TestTranslateItemFallsBackWhenSplitFails(t *testing.T) {
	var combinedCalls, titleCalls, abstractCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := readForm(t, w, r)
		if params == nil {
			return
		}
		text := params.Get("SourceText")

		switch {
		case strings.Contains(text, pairSeparator):
			atomic.AddInt32(&combinedCalls, 1)
			// 模拟引擎把分隔符吃掉：返回一整段、无法拆分的文本。
			writeTranslated(w, "标题和摘要被合并成了一整段")
		case text == "A Title":
			atomic.AddInt32(&titleCalls, 1)
			writeTranslated(w, "标题译文")
		default:
			atomic.AddInt32(&abstractCalls, 1)
			writeTranslated(w, "摘要译文")
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	titleZh, abstractZh, err := c.TranslateItem(context.Background(), "A Title", "An Abstract")
	if err != nil {
		t.Fatalf("TranslateItem: %v", err)
	}
	if titleZh != "标题译文" || abstractZh != "摘要译文" {
		t.Errorf("降级结果 = (%q, %q), want (标题译文, 摘要译文)", titleZh, abstractZh)
	}
	if combinedCalls != 1 {
		t.Errorf("合并翻译请求 %d 次, want 1", combinedCalls)
	}
	if titleCalls != 1 || abstractCalls != 1 {
		t.Errorf("降级后 标题/摘要 请求次数 = %d/%d, want 1/1", titleCalls, abstractCalls)
	}
}

// TestTranslateItemFallbackPartialFailure 降级后只有一边成功时，返回成功的一边，另一边留空。
func TestTranslateItemFallbackPartialFailure(t *testing.T) {
	var titleCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := readForm(t, w, r)
		if params == nil {
			return
		}
		text := params.Get("SourceText")

		switch {
		case strings.Contains(text, pairSeparator):
			writeTranslated(w, "无法拆分的一整段")
		case text == "A Title":
			atomic.AddInt32(&titleCalls, 1)
			http.Error(w, "title always fails", http.StatusBadGateway)
		default:
			writeTranslated(w, "摘要译文")
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	titleZh, abstractZh, err := c.TranslateItem(context.Background(), "A Title", "An Abstract")
	if err != nil {
		t.Fatalf("只有一边失败时不应报错: %v", err)
	}
	if titleZh != "" {
		t.Errorf("标题译文应为空, 实际 %q", titleZh)
	}
	if abstractZh != "摘要译文" {
		t.Errorf("摘要译文 = %q, want 摘要译文", abstractZh)
	}
	if got := atomic.LoadInt32(&titleCalls); got != 4 {
		t.Errorf("标题请求 %d 次, want 4（失败 1 次 + 重试 3 次）", got)
	}
}

// TestTranslateItemFallbackBothFail 降级后两边都失败才返回错误。
func TestTranslateItemFallbackBothFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := readForm(t, w, r)
		if params == nil {
			return
		}
		if strings.Contains(params.Get("SourceText"), pairSeparator) {
			writeTranslated(w, "无法拆分的一整段")
			return
		}
		http.Error(w, "always fails", http.StatusBadGateway)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	if _, _, err := c.TranslateItem(context.Background(), "A Title", "An Abstract"); err == nil {
		t.Fatal("两边都失败时期望报错，实际返回 nil")
	}
}

// TestTranslateSplitsLongText 超过接口 5000 字符上限的文本要被拆成多次请求，
// 每次都不超限，且拼接后与逐段结果一致。
func TestTranslateSplitsLongText(t *testing.T) {
	const limit = 100

	var (
		mu    sync.Mutex
		texts []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := readForm(t, w, r)
		if params == nil {
			return
		}
		text := params.Get("SourceText")
		if runeLen(text) > limit {
			// 真实接口会对超长文本返回 10008。
			writeAPIError(w, 10008, "The specified string is too long.")
			return
		}
		mu.Lock()
		texts = append(texts, text)
		mu.Unlock()
		writeTranslated(w, "译["+text+"]")
	}))
	defer srv.Close()

	// 由多个句子组成的长文本，按 ". " 切分点应当能整句切开。
	sentence := "This is a sentence about retrieval augmented generation research. "
	long := strings.Repeat(sentence, 8) // 约 500 字符

	c := newTestClient(t, srv.URL, func(cfg *Config) { cfg.MaxTextLength = limit })
	out, err := c.Translate(context.Background(), long, LangAuto, LangChinese)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}

	mu.Lock()
	got := append([]string(nil), texts...)
	mu.Unlock()

	if len(got) < 2 {
		t.Fatalf("分段请求数 = %d, want ≥2（文本 %d 字符 > 上限 %d）", len(got), runeLen(long), limit)
	}
	for i, text := range got {
		if runeLen(text) > limit {
			t.Errorf("第 %d 段 %d 字符，超过上限 %d: %q", i+1, runeLen(text), limit, text)
		}
	}

	// 拼接后的请求文本必须与原文本完全一致：分段不能丢字、不能改字。
	// 单段之间没有分隔符（文本本身不带 pairSeparator），因此直接串接。
	if joined := strings.Join(got, ""); joined != long {
		t.Errorf("分段拼接结果与原文不一致\n got = %q\nwant = %q", joined, long)
	}

	// 译文是把各段译文按顺序拼起来的。
	want := "译[" + strings.Join(got, "]译[") + "]"
	if out != want {
		t.Errorf("译文 = %q, want %q", out, want)
	}
}

// TestTranslateItemSeparatorSurvivesChunking 超长文本分段后仍要能拆回标题与摘要。
//
// 分隔符必须完整落在某一个分段里，翻译后才能把它拆回标题与摘要。
// 真实引擎会把分隔符当成普通文本原样保留，这里就用"原样返回 + 译文标记"来模拟，
// 从而能真实反映分段方案在接口上的效果。
func TestTranslateItemSeparatorSurvivesChunking(t *testing.T) {
	const limit = 80
	longAbstract := strings.Repeat("Sentence about retrieval augmented generation. ", 20) // 约 960 字符
	title := strings.Repeat("Very long paper title about search. ", 3)                    // 约 108 字符

	var (
		mu       sync.Mutex
		combined int32
		fallback int32
		chunks   []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := readForm(t, w, r)
		if params == nil {
			return
		}
		text := params.Get("SourceText")
		if runeLen(text) > limit {
			writeAPIError(w, 10008, "The specified string is too long.")
			return
		}
		mu.Lock()
		chunks = append(chunks, text)
		mu.Unlock()

		if strings.Contains(text, pairSeparator) {
			atomic.AddInt32(&combined, 1)
		}
		// 降级路径的特征：整段原文（完整标题或完整摘要）单独发一次请求。
		// 分段请求虽然也可能不带分隔符，但都是被切短的片段，不会等于整段原文。
		if text == title || text == longAbstract {
			atomic.AddInt32(&fallback, 1)
		}
		// 模拟引擎：原样保留分隔符，只在整体外层加一个译文标记。
		writeTranslated(w, "【译】"+text)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, func(cfg *Config) { cfg.MaxTextLength = limit })
	titleZh, abstractZh, err := c.TranslateItem(context.Background(), title, longAbstract)
	if err != nil {
		t.Fatalf("TranslateItem: %v", err)
	}

	if got := atomic.LoadInt32(&combined); got != 1 {
		t.Errorf("含分隔符的请求 %d 次, want 1（分隔符应完整保留在某一段里，且只出现一次）", got)
	}
	// 没有走"标题与摘要分别翻译"的降级路径，说明分隔符被完整保留、成功拆回。
	if got := atomic.LoadInt32(&fallback); got != 0 {
		t.Errorf("降级请求 %d 次, want 0（分隔符应被完整保留，无需降级）", got)
	}

	// 发出去的每一段都不超限，且拼接回来仍是原文（含分隔符，且只出现一次）。
	mu.Lock()
	got := append([]string(nil), chunks...)
	mu.Unlock()
	for i, chunk := range got {
		if runeLen(chunk) > limit {
			t.Errorf("第 %d 段 %d 字符，超过上限 %d", i+1, runeLen(chunk), limit)
		}
	}
	if joined := strings.Join(got, ""); joined != title+pairSeparator+longAbstract {
		t.Errorf("分段拼接结果与原文不一致，分隔符丢失或被改动")
	}
	if n := strings.Count(strings.Join(got, ""), pairSeparator); n != 1 {
		t.Errorf("分隔符出现 %d 次, want 1", n)
	}

	// 拆回来的两段分别落在标题与摘要上：标题段应当是短的，摘要段是长的。
	if !strings.Contains(titleZh, "Very long paper title") {
		t.Errorf("标题译文 = %q，应含原标题内容", titleZh)
	}
	if !strings.Contains(abstractZh, "Sentence about retrieval") {
		t.Errorf("摘要译文 = %q，应含原摘要内容", abstractZh)
	}
	if len([]rune(titleZh)) >= len([]rune(abstractZh)) {
		t.Errorf("标题译文(%d 字符) 不该比摘要译文(%d 字符) 还长，说明拆分位置错了",
			len([]rune(titleZh)), len([]rune(abstractZh)))
	}
}

// TestTranslateItemKeepsSeparatorInsideSegment 超长文本分段后，标题与摘要仍能拆开：
// 分隔符必须原样保留在某个分段里，否则合并翻译就永远拆不回来。
func TestTranslateItemKeepsSeparatorInsideSegment(t *testing.T) {
	const limit = 60
	longAbstract := strings.Repeat("Deep learning for search. ", 6) // 约 160 字符

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := readForm(t, w, r)
		if params == nil {
			return
		}
		text := params.Get("SourceText")
		if runeLen(text) > limit {
			writeAPIError(w, 10008, "The specified string is too long.")
			return
		}
		writeTranslated(w, "译:"+text)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, func(cfg *Config) { cfg.MaxTextLength = limit })
	titleZh, abstractZh, err := c.TranslateItem(context.Background(), "A Title", longAbstract)
	if err != nil {
		t.Fatalf("TranslateItem: %v", err)
	}
	if titleZh == "" || abstractZh == "" {
		t.Fatalf("标题/摘要译文不应为空: (%q, %q)", titleZh, abstractZh)
	}
	if !strings.Contains(titleZh, "A Title") {
		t.Errorf("标题译文 = %q, 应含原标题", titleZh)
	}
	if strings.Contains(titleZh, pairSeparator) {
		t.Errorf("标题译文不该残留分隔符: %q", titleZh)
	}
}

// TestSplitSegmentsNeverBreaksSeparator 分段绝不能把分隔符切成两半。
//
// 这是"宁可多花一次调用，也不能产出永远拆不开的译文"的底线：
// 分隔符一旦被切断，TranslateItem 里的 splitPair 就永远失败，
// 只能整段降级重新翻译。
func TestSplitSegmentsNeverBreaksSeparator(t *testing.T) {
	texts := []string{
		"short",
		"title" + pairSeparator + "abstract",
		strings.Repeat("a", 50) + pairSeparator + strings.Repeat("b", 50),
		strings.Repeat("word ", 40) + pairSeparator + strings.Repeat("词 ", 40),
		"Paper title. " + pairSeparator + strings.Repeat("Sentence about retrieval. ", 20),
		strings.Repeat("Sentence about retrieval. ", 20) + pairSeparator + "Paper title.",
	}

	for i, text := range texts {
		for _, limit := range []int{10, 25, 60, 200, 4000} {
			segments := splitSegments(text, pairSeparator, limit)

			// 1) 分段拼接起来必须与原文完全一致（不多字、不少字、不改顺序）。
			if joined := strings.Join(segments, ""); joined != text {
				t.Errorf("用例 %d 上限 %d：分段拼接结果与原文不一致\n got = %q\nwant = %q", i, limit, joined, text)
			}

			for j, seg := range segments {
				// 2) 每段都不能超过接口上限。
				if runeLen(seg) > limit {
					t.Errorf("用例 %d 上限 %d：第 %d 段 %d 字符超限: %q", i, limit, j+1, runeLen(seg), seg)
				}
				// 3) 分隔符要么完整出现，要么完全不出现——不能只剩半截。
				//    上限大于分隔符长度时（正常配置）不允许出现"完全不出现"以外的残缺形态。
				if runeLen(pairSeparator) < limit {
					for k := 1; k < len(pairSeparator); k++ {
						fragment := pairSeparator[:k]
						if strings.HasSuffix(seg, fragment) && !strings.HasSuffix(seg, pairSeparator) {
							t.Errorf("用例 %d 上限 %d：第 %d 段以分隔符前缀 %q 结尾（分隔符被切断）: %q",
								i, limit, j+1, fragment, seg)
						}
					}
				}
			}

			// 4) 分隔符所在的段必须能被 splitPair 正确拆开：把含分隔符的那一段
			//    单独拿出来，按分隔符拆开应该恰好得到两半。
			for _, seg := range segments {
				if !strings.Contains(seg, pairSeparator) {
					continue
				}
				if n := strings.Count(seg, pairSeparator); n != 1 {
					t.Errorf("用例 %d 上限 %d：含分隔符的段出现了 %d 次分隔符: %q", i, limit, n, seg)
				}
			}
		}
	}
}

// TestGlossary 术语在请求里被替换成占位符，译文里再还原成固定译法。
func TestGlossary(t *testing.T) {
	var gotText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := readForm(t, w, r)
		if params == nil {
			return
		}
		gotText = params.Get("SourceText")
		// 模拟引擎：不改动占位符，只加个前缀。
		writeTranslated(w, "译文 "+gotText)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, withGlossary([]GlossaryItem{{Term: "RAG", Translation: "检索增强生成"}}))
	out, err := c.Translate(context.Background(), "RAG works", LangAuto, LangChinese)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if strings.Contains(gotText, "RAG") {
		t.Errorf("请求文本仍含原术语（未被占位符替换）: %q", gotText)
	}
	if !strings.Contains(gotText, glossaryPlaceholder(0)) {
		t.Errorf("请求文本应含占位符 %q，实际 %q", glossaryPlaceholder(0), gotText)
	}
	if !strings.Contains(out, "检索增强生成") {
		t.Errorf("译文应含术语的固定译法，实际 %q", out)
	}
	if strings.Contains(out, glossaryPlaceholder(0)) {
		t.Errorf("译文残留占位符: %q", out)
	}
}

// TestGlossaryLongestTermFirst 长术语先替换，避免短术语吃掉长术语的前缀。
func TestGlossaryLongestTermFirst(t *testing.T) {
	var gotText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := readForm(t, w, r)
		if params == nil {
			return
		}
		gotText = params.Get("SourceText")
		writeTranslated(w, gotText)
	}))
	defer srv.Close()

	glossary := []GlossaryItem{
		{Term: "RAG", Translation: "检索增强生成"},
		{Term: "RAGAS", Translation: "检索增强生成评估"},
	}
	c := newTestClient(t, srv.URL, withGlossary(glossary))
	out, err := c.Translate(context.Background(), "RAGAS is better than RAG", LangAuto, LangChinese)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if strings.Contains(gotText, "RAGAS") || strings.Contains(gotText, "RAG") {
		t.Errorf("请求文本应已全部替换为占位符: %q", gotText)
	}
	if !strings.Contains(out, "检索增强生成评估") {
		t.Errorf("译文应含 RAGAS 的译法，实际 %q", out)
	}
	if !strings.Contains(out, "检索增强生成") {
		t.Errorf("译文应含 RAG 的译法，实际 %q", out)
	}
}

// TestGlossaryRestoreIgnoresCase 引擎可能改动占位符的大小写，还原必须忽略大小写。
//
// 实测 alimt 会把占位符变形（历史版本里 XGLOSSARYTERMX0X 回来过 Xglossarydermx0x）；
// 按精确匹配还原的话占位符会原样留在译文里写进库——比术语没生效更糟。
// 现在的占位符是纯小写字母数字（zqmarkerq0qz），大小写变形已不太可能，
// 但这个容错仍然保留。
func TestGlossaryRestoreIgnoresCase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := readForm(t, w, r)
		if params == nil {
			return
		}
		// 模拟引擎：原样返回，但把整段文本大写化
		writeTranslated(w, strings.ToUpper(params.Get("SourceText")))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, withGlossary([]GlossaryItem{{Term: "RAG", Translation: "检索增强生成"}}))
	out, err := c.Translate(context.Background(), "RAG improves annotation.", LangAuto, LangChinese)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if strings.Contains(strings.ToLower(out), "marker") {
		t.Errorf("译文残留占位符（大小写变形没还原）: %q", out)
	}
	if !strings.Contains(out, "检索增强生成") {
		t.Errorf("译文应含术语的固定译法，实际 %q", out)
	}
}

// TestGlossaryPlaceholderSurvivesEngine 回归：占位符必须不含会被翻译的"词"。
//
// 老实现用 XGLOSSARYTERMX0X，alimt 会把其中的 GLOSSARY 直接译成"术语表"
// （实测返回 "X术语表X0X"），还原时匹配不上，占位符就漏进了中文字段。
// 这里断言占位符不含词典词、不含可被拆分的标点、且保持全小写。
func TestGlossaryPlaceholderSurvivesEngine(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		p := glossaryPlaceholder(i)
		if p == "" {
			t.Fatal("占位符为空")
		}
		for _, bad := range []string{"glossary", "GLOSSARY", "_", "-", " ", "MT", "term"} {
			if strings.Contains(strings.ToLower(p), strings.ToLower(bad)) {
				t.Errorf("占位符 %q 含易被引擎处理的片段 %q", p, bad)
			}
		}
		if p != strings.ToLower(p) {
			t.Errorf("占位符 %q 应全小写（大写缩写容易被引擎改写）", p)
		}
		if !strings.ContainsAny(p, "0123456789") {
			t.Errorf("占位符 %q 应含下标数字，便于人肉排查", p)
		}
		if seen[p] {
			t.Errorf("占位符 %q 重复，多术语会互相覆盖", p)
		}
		seen[p] = true
	}
}

// TestGlossaryPlaceholderTranslatedByEngine 模拟 alimt 把占位符里的"词典词"译掉的情形：
// 现在的占位符不含这类词，所以这种改写不该再发生（用旧占位符时这里会失败）。
func TestGlossaryPlaceholderTranslatedByEngine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := readForm(t, w, r)
		if params == nil {
			return
		}
		// 模拟"引擎把 GLOSSARY / TERM 之类的词典词译成中文"
		out := params.Get("SourceText")
		out = strings.ReplaceAll(out, "GLOSSARY", "术语表")
		out = strings.ReplaceAll(out, "TERM", "术语")
		writeTranslated(w, out)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, withGlossary([]GlossaryItem{{Term: "RAG", Translation: "检索增强生成"}}))
	out, err := c.Translate(context.Background(), "RAG is useful.", LangAuto, LangChinese)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if strings.Contains(out, "术语表") || strings.Contains(strings.ToLower(out), "marker") {
		t.Errorf("占位符被引擎改写后没能还原: %q", out)
	}
	if !strings.Contains(out, "检索增强生成") {
		t.Errorf("译文应含术语的固定译法，实际 %q", out)
	}
}

// TestGlossaryAbsentTermUntouched 原文里没有的术语不该在译文里凭空出现。
func TestGlossaryAbsentTermUntouched(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := readForm(t, w, r)
		if params == nil {
			return
		}
		writeTranslated(w, "普通译文")
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, withGlossary([]GlossaryItem{{Term: "RAG", Translation: "检索增强生成"}}))
	out, err := c.Translate(context.Background(), "no term here", LangAuto, LangChinese)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if out != "普通译文" {
		t.Errorf("译文 = %q, want 普通译文（不该被术语表改写）", out)
	}
}

// TestTranslateHonorsSemaphore 批量翻译时同时在飞的请求数不超过闸门容量。
// 接口的 QPS / 并发额度有限，并发数一旦失控就会换来限流与重试。
func TestTranslateHonorsSemaphore(t *testing.T) {
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

		writeTranslated(w, "译文")
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, func(cfg *Config) { cfg.Concurrency = limit })

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Translate(context.Background(), "hello", LangAuto, LangChinese); err != nil {
				t.Errorf("Translate: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := peak.Load(); got > limit {
		t.Errorf("同时在飞的翻译请求峰值 = %d, 超过闸门容量 %d", got, limit)
	}
	if got := peak.Load(); got < 2 {
		t.Errorf("峰值 = %d, 闸门可能把并发压成了串行", got)
	}
}

// TestTranslateSemaphoreUnlimitedByDefault 并发上限配成负数时闸门为 nil（不限流）。
func TestTranslateSemaphoreUnlimitedByDefault(t *testing.T) {
	c := newTestClient(t, "http://127.0.0.1:0", func(cfg *Config) { cfg.Concurrency = -1 })
	if c.gate != nil {
		t.Errorf("Concurrency<0 时 gate 应为 nil, 实际 %v", c.gate)
	}
}

// TestTranslateHonorsRateLimit 翻译请求按速率放行。
// 通用版的额度是 QPS 50，但限速机制本身仍然是可配置的，这里用 2 请求/秒 + 桶容量 3 验证：
// 10 个并发请求的整体跨度至少要 3 秒。
func TestTranslateHonorsRateLimit(t *testing.T) {
	var arrivals []time.Time
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		arrivals = append(arrivals, time.Now())
		mu.Unlock()
		writeTranslated(w, "译文")
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, func(cfg *Config) {
		cfg.QPS, cfg.Burst = 2, 3 // 每秒 2 个，允许 3 个突发
	})

	const workers = 10
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Translate(context.Background(), "hello", LangAuto, LangChinese); err != nil {
				t.Errorf("Translate: %v", err)
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	got := append([]time.Time(nil), arrivals...)
	mu.Unlock()

	if len(got) != workers {
		t.Fatalf("请求数 = %d, want %d", len(got), workers)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].Before(got[j]) })

	// 直接断言限速器的不变式：相邻两次请求至少隔一个间隔（2 RPS → 500ms），
	// 突发只允许出现在开头、且不超过桶容量 3。
	// 这个断言与调度顺序无关，因此不依赖 goroutine 的调度抖动。
	const interval = 500 * time.Millisecond
	const burst = 3
	minGap := time.Duration(float64(interval) * 0.75) // 吸收调度抖动

	for i := 1; i < len(got); i++ {
		gap := got[i].Sub(got[i-1])
		if i >= burst && gap < minGap {
			t.Errorf("第 %d 与第 %d 次请求只隔了 %v（< %v），限速器没有生效",
				i, i+1, gap, minGap)
		}
	}

	// 总跨度必须符合"10 个请求、每秒 2 个"的量级（约 4.5s）：
	// 太短说明突发，太长说明排队算错了（会白等）。
	span := got[len(got)-1].Sub(got[0])
	if span < 3*time.Second || span > 8*time.Second {
		t.Errorf("10 个请求的总跨度 = %v, 期望 3s~8s（理论约 4.5s）", span)
	}
	t.Logf("跨度 = %v（理论约 4.5s），前三个间隔 = %v/%v",
		span, got[1].Sub(got[0]), got[2].Sub(got[1]))
}

// TestTranslateLimiterUnlimitedByDefault QPS 配成负数时关闭限速（限流器为 nil）。
func TestTranslateLimiterUnlimitedByDefault(t *testing.T) {
	c := newTestClient(t, "http://127.0.0.1:0", func(cfg *Config) { cfg.QPS = -1 })
	if c.limiter != nil {
		t.Errorf("QPS<0 时应关闭限速, 实际 %v", c.limiter)
	}
}

// TestTranslateZeroQPSMeansProviderDefault QPS=0 的含义是"用 provider 默认值"，
// 而不是"不限速"：alimt 的默认是 QPS 50、并发 8。
// 这个区分很关键——若把 0 当成不限速，默认配置就会全速打接口。
func TestTranslateZeroQPSMeansProviderDefault(t *testing.T) {
	c := newTestClient(t, "http://127.0.0.1:0", nil)
	if c.limiter == nil {
		t.Fatal("QPS=0 时应回退到 provider 默认限速，实际没有限流器")
	}
	if got := c.limiter.Rate(); got != DefaultAlimtQPS {
		t.Errorf("alimt 默认 QPS = %v, want %v", got, DefaultAlimtQPS)
	}
	if got := c.gate.Cap(); got != DefaultAlimtConcurrency {
		t.Errorf("alimt 默认并发 = %d, want %d", got, DefaultAlimtConcurrency)
	}
}

// TestTranslateHighQPSIsNotSerial 默认档位（QPS 50 + 桶容量 50）下，
// 富化开头同时发出的十几个请求应当立刻放行，而不是被排队成一秒一个。
// 这正是从 qwen-mt-lite（60 RPM）换到通用版（QPS 50）要拿回来的并发能力。
func TestTranslateHighQPSIsNotSerial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTranslated(w, "译文")
	}))
	defer srv.Close()

	// 与 config.DefaultTranslateQPS / DefaultTranslateBurst 保持一致。
	c := newTestClient(t, srv.URL, func(cfg *Config) { cfg.QPS, cfg.Burst = DefaultAlimtQPS, DefaultAlimtBurst })

	const workers = 16
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Translate(context.Background(), "hello", LangAuto, LangChinese); err != nil {
				t.Errorf("Translate: %v", err)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	// 桶容量 50 > 16，所以这 16 个请求不该有任何排队等待。
	if elapsed > time.Second {
		t.Errorf("%d 个并发请求耗时 %v, 不该被限速器排队（桶容量足够）", workers, elapsed)
	}
	t.Logf("%d 个并发请求耗时 %v", workers, elapsed)
}

// TestRandomNonce 随机串长度与唯一性。
func TestRandomNonce(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		n := randomNonce()
		if n == "" {
			t.Fatal("randomNonce 返回空串")
		}
		if seen[n] {
			t.Fatalf("randomNonce 重复: %q", n)
		}
		seen[n] = true
	}
}
