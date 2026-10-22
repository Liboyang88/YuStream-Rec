package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"searcher/retry"
)

// 真实调用千问 MaaS（qwen3.8-flash）的用例，默认跳过（未提供 key 时 t.Skip）。运行方式：
//
//	SEARCHER_IT_MODEL_API_KEY=sk-xxx SEARCHER_IT_MODEL_NAME=qwen3.8-flash \
//	go test ./llm/ -run Integration -v
func TestIntegrationGenerateSummary(t *testing.T) {
	apiKey := os.Getenv("SEARCHER_IT_MODEL_API_KEY")
	modelName := os.Getenv("SEARCHER_IT_MODEL_NAME")
	if apiKey == "" || modelName == "" {
		t.Skip("未设置 SEARCHER_IT_MODEL_API_KEY / SEARCHER_IT_MODEL_NAME，跳过集成用例")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	s := NewSummarizer(apiKey, modelName)
	summary, err := s.GenerateSummary(ctx,
		"Insights into human evolution from large genetic biobanks",
		"Large genetic biobanks with deep phenotyping and whole-exome or whole-genome sequencing provide unprecedented opportunities to understand human evolution.")
	if err != nil {
		t.Fatalf("GenerateSummary: %v", err)
	}
	if strings.TrimSpace(summary) == "" {
		t.Fatal("返回了空的文献介绍")
	}
	t.Logf("文献介绍：%s", summary)
}

// TestIntegrationPermanentErrorNotRetried 无效 key 属于 4xx（配置问题），
// 应当立即失败，而不是等 3 次退避重试（5s+10s+15s=30s）。
func TestIntegrationPermanentErrorNotRetried(t *testing.T) {
	if os.Getenv("SEARCHER_IT_MODEL_NAME") == "" {
		t.Skip("未设置 SEARCHER_IT_MODEL_NAME，跳过集成用例")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	s := NewSummarizer("sk-invalid-key-for-integration-test", os.Getenv("SEARCHER_IT_MODEL_NAME"),
		WithRetryConfig(retry.Config{MaxRetries: 3, Backoff: 5 * time.Second}))

	start := time.Now()
	_, err := s.GenerateSummary(ctx, "A Title", "An Abstract")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("无效 key 期望返回错误，实际返回 nil")
	}
	if elapsed > 20*time.Second {
		t.Errorf("耗时 %v，看起来对 4xx 做了重试（不应重试）", elapsed)
	}
	t.Logf("无效 key 在 %v 内失败（未重试）: %v", elapsed, err)
}
