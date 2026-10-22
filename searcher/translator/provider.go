package translator

import (
	"fmt"
	"net/http"

	"searcher/retry"
	"searcher/semaphore"
)

// Config 是构造翻译客户端的配置。
//
// 两种 provider 需要的凭据不同，因此这里把两者都列出来，由 New 按 Provider 各取所需：
//
//	alimt   用 RAM 的 AccessKey ID/Secret
//	qwenmt  用百炼（DashScope）的 ApiKey
type Config struct {
	// Provider 选择实现；空值表示 ProviderAliyunMT。
	Provider Provider

	// alimt 的凭据。
	AccessKeyID     string
	AccessKeySecret string

	// qwenmt 的凭据。
	APIKey string

	// QPS / Burst 是限速（每秒放行数 + 桶容量）；负值表示不限速。
	//
	// 注意：**0 表示"用该 provider 的默认值"**，而不是"不限速"——
	// 两种 provider 的额度相差两个数量级，默认值必须按 provider 分别取：
	// alimt 默认 QPS 50、qwenmt 默认 QPS 1，见 resolveLimits。
	QPS   float64
	Burst int

	// Concurrency 是同时在飞的请求数上限（并发闸门）；0 表示用默认值，
	// 负值表示不限流。Qwen-MT 默认会压到 DefaultQwenConcurrency。
	Concurrency int

	// 以下为可选覆盖项，一般留空。
	Endpoint      string
	Model         string // 仅 qwenmt 有意义
	Glossary      []GlossaryItem
	Retry         *retry.Config
	HTTPClient    *http.Client
	MaxTextLength int // 仅 alimt 有意义（测试分段用）
}

// New 按 cfg.Provider 构造翻译客户端。
//
// 两个 provider 共用同一套"合并标题与摘要 → 拆回 → 失败降级"的逻辑与术语表实现，
// 差别只有认证方式、协议形态、以及限流额度。
func New(cfg Config) (Client, error) {
	provider, ok := NormalizeProvider(string(cfg.Provider))
	if !ok {
		return nil, fmt.Errorf("未知的翻译 provider %q，可选：%v", cfg.Provider, Providers())
	}

	o := newOptions()
	if cfg.Glossary != nil {
		o.glossary = cfg.Glossary
	}
	if cfg.Retry != nil {
		o.retry = *cfg.Retry
	}
	if cfg.HTTPClient != nil {
		o.httpClient = cfg.HTTPClient
	}
	if cfg.Endpoint != "" {
		o.endpoint = cfg.Endpoint
	}
	if cfg.Model != "" {
		o.model = cfg.Model
	}
	if cfg.MaxTextLength > 0 {
		o.maxTextLength = cfg.MaxTextLength
	}

	limits := resolveLimits(provider, cfg)
	o.limiter = semaphore.NewLimiter(limits.qps, limits.burst)
	o.gate = semaphore.New(limits.concurrency)

	switch provider {
	case ProviderQwenMT:
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("provider %s 需要 APIKey（百炼 DashScope 的 ApiKey）", provider)
		}
		return newQwenClient(cfg.APIKey, o), nil
	default:
		if cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
			return nil, fmt.Errorf("provider %s 需要 AccessKeyID 与 AccessKeySecret（RAM 账号的 AccessKey）", provider)
		}
		return newAlimtClient(cfg.AccessKeyID, cfg.AccessKeySecret, o), nil
	}
}

// limits 是一组解析好的限流参数。
type limits struct {
	qps         float64
	burst       int
	concurrency int
}

// resolveLimits 按 provider 把"未配置"补成各自的默认值。
//
// 为什么必须分开默认：alimt 的额度是 QPS 50，qwen-mt-lite 只有 60 RPM（≈1 RPS）。
// 用同一套默认值的话，要么把 alimt 白白限成每秒 1 个（比之前更慢），
// 要么让 Qwen-MT 每秒发 50 个而不断触发限流。
//
// 负值语义（与 config 包一致）：QPS < 0 表示不限速；Concurrency < 0 表示不限并发。
func resolveLimits(provider Provider, cfg Config) limits {
	out := limits{}
	switch provider {
	case ProviderQwenMT:
		out.qps, out.burst, out.concurrency = DefaultQwenQPS, DefaultQwenBurst, DefaultQwenConcurrency
	default:
		out.qps, out.burst, out.concurrency = DefaultAlimtQPS, DefaultAlimtBurst, DefaultAlimtConcurrency
	}

	if cfg.QPS > 0 {
		out.qps = cfg.QPS
	}
	if cfg.QPS < 0 {
		out.qps, out.burst = 0, 0 // 不限速
	}
	if cfg.Burst > 0 {
		out.burst = cfg.Burst
	}
	if cfg.Concurrency != 0 {
		out.concurrency = cfg.Concurrency // 负值即不限流（semaphore.New 对 <=0 返回 nil）
	}
	return out
}

// 各 provider 的默认限流参数。
const (
	// DefaultAlimtQPS / DefaultAlimtBurst 对应机器翻译通用版文档写明的 "QPS 限制 50"。
	// 桶容量取与 QPS 同量级，让富化开头同时发出的十几条翻译立刻放行，
	// 而不是被排成整齐的一秒一串。
	DefaultAlimtQPS   = 50.0
	DefaultAlimtBurst = 50

	// DefaultAlimtConcurrency 是 alimt 默认的并发上限。
	DefaultAlimtConcurrency = 8
)
