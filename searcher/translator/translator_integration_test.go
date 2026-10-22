package translator

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"searcher/retry"
)

// 真实调用阿里云机器翻译通用版（alimt TranslateGeneral）的用例，
// 默认跳过（未提供 AccessKey 时 t.Skip）。运行方式：
//
//	SEARCHER_IT_ALIYUN_MT_ACCESS_KEY_ID=LTAIxxx \
//	SEARCHER_IT_ALIYUN_MT_ACCESS_KEY_SECRET=xxx \
//	go test ./translator/ -run Integration -v
func TestIntegrationTranslateItem(t *testing.T) {
	accessKeyID := os.Getenv("SEARCHER_IT_ALIYUN_MT_ACCESS_KEY_ID")
	accessKeySecret := os.Getenv("SEARCHER_IT_ALIYUN_MT_ACCESS_KEY_SECRET")
	if accessKeyID == "" || accessKeySecret == "" {
		t.Skip("未设置 SEARCHER_IT_ALIYUN_MT_ACCESS_KEY_ID / _SECRET，跳过集成用例")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	c, err := New(Config{Provider: ProviderAliyunMT, AccessKeyID: accessKeyID, AccessKeySecret: accessKeySecret})
	if err != nil {
		t.Fatalf("构造客户端: %v", err)
	}
	titleZh, abstractZh, err := c.TranslateItem(ctx,
		"Insights into human evolution from large genetic biobanks",
		"Large genetic biobanks with deep phenotyping provide unprecedented opportunities to understand human evolution.")
	if err != nil {
		t.Fatalf("TranslateItem: %v", err)
	}
	if strings.TrimSpace(titleZh) == "" || strings.TrimSpace(abstractZh) == "" {
		t.Fatalf("译文为空: title=%q abstract=%q", titleZh, abstractZh)
	}
	t.Logf("标题译文：%s", titleZh)
	t.Logf("摘要译文：%s", abstractZh)

	// 术语表（客户端占位符方案）在真实接口上也要生效。
	if !strings.Contains(abstractZh+titleZh, "检索增强生成") {
		t.Log("提示：本次调用未涉及术语 RAG，术语替换未参与验证")
	}
}

// TestIntegrationLongTextSplit 真实接口对超过 5000 字符的文本会返回 10008，
// 这里用一段接近上限的文本验证分段翻译能正常返回结果。
func TestIntegrationLongTextSplit(t *testing.T) {
	accessKeyID := os.Getenv("SEARCHER_IT_ALIYUN_MT_ACCESS_KEY_ID")
	accessKeySecret := os.Getenv("SEARCHER_IT_ALIYUN_MT_ACCESS_KEY_SECRET")
	if accessKeyID == "" || accessKeySecret == "" {
		t.Skip("未设置 SEARCHER_IT_ALIYUN_MT_ACCESS_KEY_ID / _SECRET，跳过集成用例")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	sentence := "Retrieval augmented generation improves factual consistency of large language models. "
	long := strings.Repeat(sentence, 100) // 约 8700 字符，必须分段

	c, err := New(Config{Provider: ProviderAliyunMT, AccessKeyID: accessKeyID, AccessKeySecret: accessKeySecret})
	if err != nil {
		t.Fatalf("构造客户端: %v", err)
	}
	out, err := c.Translate(ctx, long, LangAuto, LangChinese)
	if err != nil {
		t.Fatalf("长文本 Translate: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("长文本译文为空")
	}
	t.Logf("原文 %d 字符 → 译文 %d 字符", runeLen(long), runeLen(out))
}

// TestIntegrationPermanentErrorNotRetried 签名错误属于 4xx（配置问题），
// 应当立即失败，而不是等 3 次退避重试（5s+10s+15s=30s）。
func TestIntegrationPermanentErrorNotRetried(t *testing.T) {
	// 这条用例会真的请求机器翻译接口（用无效 AccessKey），因此同样只在显式提供
	// 凭据时才跑，避免默认 go test 依赖外网。
	if os.Getenv("SEARCHER_IT_ALIYUN_MT_ACCESS_KEY_ID") == "" {
		t.Skip("未设置 SEARCHER_IT_ALIYUN_MT_ACCESS_KEY_ID，跳过集成用例")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	slowRetry := retry.Config{MaxRetries: 3, Backoff: 5 * time.Second}
	c, err := New(Config{
		Provider:        ProviderAliyunMT,
		AccessKeyID:     "LTAI-invalid-access-key-id-for-test",
		AccessKeySecret: "invalid-access-key-secret",
		Retry:           &slowRetry,
	})
	if err != nil {
		t.Fatalf("构造客户端: %v", err)
	}

	start := time.Now()
	_, _, err = c.TranslateItem(ctx, "A Title", "An Abstract")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("无效 AccessKey 期望返回错误，实际返回 nil")
	}
	if elapsed > 20*time.Second {
		t.Errorf("耗时 %v，看起来对 4xx 做了重试（不应重试）", elapsed)
	}
	t.Logf("无效 AccessKey 在 %v 内失败（未重试）: %v", elapsed, err)
}
