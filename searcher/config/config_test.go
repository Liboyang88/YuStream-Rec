package config

import (
	"os"
	"strings"
	"testing"
)

// TestNormalizeConcurrency 并发配置的默认值补全规则：
// 0（未配置）→ 默认值；正数/负数（显式指定，负数表示不限流）保持原样。
//
// 例外：TranslationConcurrency 的 0 **保持 0**，含义是"用当前 provider 的默认值"
// （alimt 8 / qwenmt 2）——两种 provider 的额度差两个数量级，config 包不认识这个语义，
// 补默认值的职责在 translator.resolveLimits。
func TestNormalizeConcurrency(t *testing.T) {
	cases := []struct {
		name          string
		in            Config
		wantEnrich    int
		wantSummary   int
		wantTranslate int
	}{
		{
			name:          "全部未配置",
			in:            Config{},
			wantEnrich:    DefaultEnrichConcurrency,
			wantSummary:   DefaultAPIConcurrency,
			wantTranslate: 0, // 交给 provider 决定
		},
		{
			name:          "显式配置保持不变",
			in:            Config{EnrichConcurrency: 16, SummaryConcurrency: 4, TranslationConcurrency: 2},
			wantEnrich:    16,
			wantSummary:   4,
			wantTranslate: 2,
		},
		{
			name:          "负数表示不限流，不被覆盖",
			in:            Config{EnrichConcurrency: -1, SummaryConcurrency: -1, TranslationConcurrency: -1},
			wantEnrich:    -1,
			wantSummary:   -1,
			wantTranslate: -1,
		},
		{
			name:          "部分配置",
			in:            Config{EnrichConcurrency: 3},
			wantEnrich:    3,
			wantSummary:   DefaultAPIConcurrency,
			wantTranslate: 0, // 未配置 → 交给 provider
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.in
			cfg.NormalizeConcurrency()
			if cfg.EnrichConcurrency != tc.wantEnrich {
				t.Errorf("EnrichConcurrency = %d, want %d", cfg.EnrichConcurrency, tc.wantEnrich)
			}
			if cfg.SummaryConcurrency != tc.wantSummary {
				t.Errorf("SummaryConcurrency = %d, want %d", cfg.SummaryConcurrency, tc.wantSummary)
			}
			if cfg.TranslationConcurrency != tc.wantTranslate {
				t.Errorf("TranslationConcurrency = %d, want %d", cfg.TranslationConcurrency, tc.wantTranslate)
			}
		})
	}
}

// TestLoadAppliesConcurrencyDefaults 从 .env 读取时同样补全默认值（缺键不应留下 0 并发）。
func TestLoadAppliesConcurrencyDefaults(t *testing.T) {
	path := t.TempDir() + "/.env"
	writeFile(t, path, "MODEL_NAME=test-model\nMODEL_API_KEY=sk-test\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.EnrichConcurrency != DefaultEnrichConcurrency {
		t.Errorf("EnrichConcurrency = %d, want %d", cfg.EnrichConcurrency, DefaultEnrichConcurrency)
	}
	// summary 有固定默认值；翻译的并发默认值按 provider 决定，这里仍是 0（表示未配置）。
	if cfg.SummaryConcurrency != DefaultAPIConcurrency || cfg.TranslationConcurrency != 0 {
		t.Errorf("接口并发默认值 = %d/%d, want %d/0（翻译交给 provider）",
			cfg.SummaryConcurrency, cfg.TranslationConcurrency, DefaultAPIConcurrency)
	}
}

// TestLoadReadsConcurrencyFromEnv .env 里显式写了的并发值要生效。
func TestLoadReadsConcurrencyFromEnv(t *testing.T) {
	path := t.TempDir() + "/.env"
	writeFile(t, path, "ENRICH_CONCURRENCY=12\nSUMMARY_CONCURRENCY=5\nTRANSLATE_CONCURRENCY=3\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.EnrichConcurrency != 12 || cfg.SummaryConcurrency != 5 || cfg.TranslationConcurrency != 3 {
		t.Errorf("并发配置 = %d/%d/%d, want 12/5/3",
			cfg.EnrichConcurrency, cfg.SummaryConcurrency, cfg.TranslationConcurrency)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写临时 .env: %v", err)
	}
}

// TestNormalizeTranslateRate 翻译限速的归一规则：
//
//	0  → 保持 0，含义是"用当前 provider 的默认值"（alimt 50 / qwenmt 1），
//	     补默认值的职责在 translator 包，因为只有它知道 provider 的额度；
//	正数 → 原样保留；
//	负数 → 归一成 0 并置"不限速"标记（0 已经被"provider 默认值"占用了，
//	     不能用它表达"不限速"）。
func TestNormalizeTranslateRate(t *testing.T) {
	cases := []struct {
		name            string
		qps             float64
		burst           int
		wantQPS         float64
		wantBurst       int
		wantUnlimited   bool
		wantResolvedQPS float64
	}{
		{"未配置保持 0（交给 provider 默认）", 0, 0, 0, 0, false, 0},
		{"显式配置保持不变", 5, 4, 5, 4, false, 5},
		{"只配 QPS", 3, 0, 3, 0, false, 3},
		{"只配 burst", 0, 2, 0, 2, false, 0},
		{"负数表示不限速", -1, 0, 0, 0, true, -1},
		{"负 burst 随 QPS 一起归一", -5, 7, 0, 0, true, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{TranslateQPS: tc.qps, TranslateBurst: tc.burst}
			cfg.NormalizeConcurrency()
			if cfg.TranslateQPS != tc.wantQPS {
				t.Errorf("TranslateQPS = %v, want %v", cfg.TranslateQPS, tc.wantQPS)
			}
			if cfg.TranslateBurst != tc.wantBurst {
				t.Errorf("TranslateBurst = %d, want %d", cfg.TranslateBurst, tc.wantBurst)
			}
			if got := cfg.TranslateUnlimited(); got != tc.wantUnlimited {
				t.Errorf("TranslateUnlimited = %v, want %v", got, tc.wantUnlimited)
			}
			if got := cfg.ResolveTranslateQPS(); got != tc.wantResolvedQPS {
				t.Errorf("ResolveTranslateQPS = %v, want %v", got, tc.wantResolvedQPS)
			}
		})
	}
}

// TestResolveTranslateProvider provider 名称归一；非法值原样返回给校验去报错。
func TestResolveTranslateProvider(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "alimt"}, // 留空默认 alimt
		{"alimt", "alimt"},
		{"ALIMT", "alimt"},
		{"qwenmt", "qwenmt"},
		{"qwen-mt", "qwenmt"},
		{"google", "google"}, // 非法值原样返回
	}
	for _, tc := range cases {
		cfg := Config{TranslateProvider: tc.in}
		if got := cfg.ResolveTranslateProvider(); got != tc.want {
			t.Errorf("ResolveTranslateProvider(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if tc.in != "google" {
			if err := cfg.ValidateTranslateProvider(); err != nil {
				t.Errorf("ValidateTranslateProvider(%q) 不该报错: %v", tc.in, err)
			}
		}
	}

	bad := Config{TranslateProvider: "google"}
	if err := bad.ValidateTranslateProvider(); err == nil {
		t.Error("非法 provider 应报错")
	} else if !strings.Contains(err.Error(), "TRANSLATE_PROVIDER") {
		t.Errorf("错误信息应提到配置键名，实际: %v", err)
	}
}

// TestLoadReadsTranslateProvider .env 里的 provider 选择要生效。
func TestLoadReadsTranslateProvider(t *testing.T) {
	path := t.TempDir() + "/.env"
	writeFile(t, path, "TRANSLATE_PROVIDER=qwenmt\nQWEN_MT_API_KEY=sk-test\nQWEN_MT_MODEL=qwen-mt-plus\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.ResolveTranslateProvider(); got != "qwenmt" {
		t.Errorf("provider = %q, want qwenmt", got)
	}
	if cfg.QwenMTApiKey != "sk-test" {
		t.Errorf("QWEN_MT_API_KEY = %q", cfg.QwenMTApiKey)
	}
	if cfg.QwenMTModel != "qwen-mt-plus" {
		t.Errorf("QWEN_MT_MODEL = %q", cfg.QwenMTModel)
	}
}

// TestLoadReadsTranslateRateFromEnv .env 里的限速配置要生效。
func TestLoadReadsTranslateRateFromEnv(t *testing.T) {
	path := t.TempDir() + "/.env"
	writeFile(t, path, "TRANSLATE_QPS=2.5\nTRANSLATE_BURST=3\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TranslateQPS != 2.5 || cfg.TranslateBurst != 3 {
		t.Errorf("限速配置 = %v/%d, want 2.5/3", cfg.TranslateQPS, cfg.TranslateBurst)
	}
}

// TestLoadReadsAliyunMTCredentials 机器翻译的 AccessKey 要能从 .env 读出来。
func TestLoadReadsAliyunMTCredentials(t *testing.T) {
	path := t.TempDir() + "/.env"
	writeFile(t, path, "ALIYUN_MT_ACCESS_KEY_ID=LTAI-test-id\nALIYUN_MT_ACCESS_KEY_SECRET=test-secret\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AliyunMTAccessKeyID != "LTAI-test-id" {
		t.Errorf("AccessKeyID = %q", cfg.AliyunMTAccessKeyID)
	}
	if cfg.AliyunMTAccessKeySecret != "test-secret" {
		t.Errorf("AccessKeySecret = %q", cfg.AliyunMTAccessKeySecret)
	}
	if !cfg.HasTranslationCredentials() {
		t.Error("HasTranslationCredentials 应为 true")
	}
}

// TestValidateForTranslation 只配一半凭据（漏填）要报错，都不配等于主动关闭翻译。
func TestValidateForTranslation(t *testing.T) {
	cases := []struct {
		name      string
		id        string
		secret    string
		wantErr   bool
		wantHas   bool
		errPhrase string
	}{
		{"都配了", "LTAI-id", "secret", false, true, ""},
		{"都没配", "", "", false, false, ""},
		{"只配了 ID", "LTAI-id", "", true, false, "ALIYUN_MT_ACCESS_KEY_SECRET"},
		{"只配了 Secret", "", "secret", true, false, "ALIYUN_MT_ACCESS_KEY_ID"},
		{"只有空白", "  ", "secret", true, false, "ALIYUN_MT_ACCESS_KEY_ID"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{AliyunMTAccessKeyID: tc.id, AliyunMTAccessKeySecret: tc.secret}
			err := cfg.ValidateForTranslation()
			if tc.wantErr {
				if err == nil {
					t.Fatal("期望报错，实际返回 nil")
				}
				if !strings.Contains(err.Error(), tc.errPhrase) {
					t.Errorf("错误信息应提到缺失的键 %q，实际: %v", tc.errPhrase, err)
				}
			} else if err != nil {
				t.Fatalf("不该报错: %v", err)
			}
			if got := cfg.HasTranslationCredentials(); got != tc.wantHas {
				t.Errorf("HasTranslationCredentials = %v, want %v", got, tc.wantHas)
			}
		})
	}
}
