// Package config 负责加载配置。
//
// 配置直接复用 litstream 后端的 .env（key 与后端完全一致），从而保证脚本连的是
// 同一套 MySQL / Milvus / embedding / LLM 服务；这里只声明脚本真正用到的字段。
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"

	"searcher/translator"
)

// Config 是脚本运行所需的全部配置。
type Config struct {
	// MySQL / Milvus：与后端共用同一套存储。
	MysqlDSN             string `mapstructure:"MYSQL_DSN"`
	MilvusAddress        string `mapstructure:"MILVUS_ADDRESS"`
	MilvusCollectionName string `mapstructure:"MILVUS_COLLECTION_NAME"`
	VecDim               int    `mapstructure:"VEC_DIM"`

	// Embedding：向量化摘要时必须与后端使用同一个模型，否则向量不在同一语义空间。
	// 模型名同样取自 .env（后端 embedutil 也用这个键），避免两边写死后悄悄漂移。
	EmbeddingModelAPIKey  string `mapstructure:"EMBEDDING_MODEL_API_KEY"`
	EmbeddingModelBaseURL string `mapstructure:"EMBEDDING_MODEL_BASE_URL_WITH_OPENAI"`
	EmbeddingModelName    string `mapstructure:"EMBEDDING_MODEL_NAME"`

	// LLM：生成文献介绍（summary）。走千问的 OpenAI 兼容接口，
	// 端点已写在 llm 包的默认值里（qwen3.8-flash 的 MaaS 地址）。
	//
	// 优先用 SUMMARY_MODEL_* 这对专属键；缺失时回退到 MODEL_NAME / MODEL_API_KEY
	//（保留旧 .env 的可用性，也方便只想配一套 key 的场景）。
	SummaryModelName   string `mapstructure:"SUMMARY_MODEL_NAME"`
	SummaryModelAPIKey string `mapstructure:"SUMMARY_MODEL_API_KEY"`
	ModelName          string `mapstructure:"MODEL_NAME"`
	ModelAPIKey        string `mapstructure:"MODEL_API_KEY"`

	// 翻译 provider 选择：alimt（机器翻译通用版，默认）或 qwenmt（百炼 Qwen-MT）。
	// 留空 = alimt。两种实现的凭据与额度都不同，见下面各组键。
	TranslateProvider string `mapstructure:"TRANSLATE_PROVIDER"`

	// alimt（阿里云机器翻译通用版）：用 RAM 的 AccessKey，
	// 与百炼的 MODEL_API_KEY / EMBEDDING_MODEL_API_KEY 不是同一套凭据。
	// 未配置时跳过翻译（不中断入库）。
	AliyunMTAccessKeyID     string `mapstructure:"ALIYUN_MT_ACCESS_KEY_ID"`
	AliyunMTAccessKeySecret string `mapstructure:"ALIYUN_MT_ACCESS_KEY_SECRET"`

	// qwenmt（百炼 Qwen-MT）：用百炼的 ApiKey；模型名可选（默认 qwen-mt-lite）。
	// 未配置时（选中 qwenmt 但缺 key）会直接报错，而不是静默跳过——
	// 明确选了 provider 却调不动，属于配置错误。
	QwenMTApiKey string `mapstructure:"QWEN_MT_API_KEY"`
	QwenMTModel  string `mapstructure:"QWEN_MT_MODEL"`

	// 并发：批量富化时同时处理的论文数，以及各接口各自的并发上限。
	// 未配置（0）时分别回退到 DefaultEnrichConcurrency / DefaultAPIConcurrency，
	// 负数表示不限流（仅建议在确认接口不限流时使用）。
	EnrichConcurrency      int `mapstructure:"ENRICH_CONCURRENCY"`
	SummaryConcurrency     int `mapstructure:"SUMMARY_CONCURRENCY"`
	TranslationConcurrency int `mapstructure:"TRANSLATE_CONCURRENCY"`

	// 翻译限速：每秒放行的翻译请求数（负数表示不限速）与桶容量（即允许的瞬时突发数）。
	//
	// **0 表示"用当前 provider 的默认值"**，因为两种 provider 的额度相差两个数量级：
	//
	//	alimt   默认 QPS 50（机器翻译通用版文档写明的上限）
	//	qwenmt  默认 QPS 1（qwen-mt-lite 只有 60 RPM ≈ 1 RPS）
	//
	// 同理 TranslationConcurrency 为 0 时：alimt 默认 8，qwenmt 默认 2。
	TranslateQPS   float64 `mapstructure:"TRANSLATE_QPS"`
	TranslateBurst int     `mapstructure:"TRANSLATE_BURST"`

	// translateUnlimited 记录"用户显式把 TRANSLATE_QPS 设成负数"这件事。
	// 不能只用 TranslateQPS==0 表达，因为 0 的含义是"用 provider 默认值"，
	// 两者必须区分开（否则关掉限速会变成退回默认限速）。
	translateUnlimited bool
}

// 并发默认值与 ingest 侧保持一致（这里复制常量而不是 import，避免 config 依赖 ingest）。
const (
	// DefaultEnrichConcurrency 是同时处理的论文数。
	DefaultEnrichConcurrency = 8
	// DefaultAPIConcurrency 是单个接口（summary / 翻译）同时在飞的请求数上限。
	DefaultAPIConcurrency = 8

	// DefaultTranslateQPS / DefaultTranslateBurst 是 alimt（机器翻译通用版）的默认限速：
	// 接口文档写明 QPS 限制 50，超出会被限流拒掉。
	// 这两个常量保留原名（历史配置与文档都按这个名字引用），语义等同于
	// "alimt provider 的默认值"；Qwen-MT 的默认值在 translator 包里
	//（translator.DefaultQwenQPS = 1），因为那里才是它的额度出处。
	DefaultTranslateQPS   = 50.0
	DefaultTranslateBurst = 50
)

// NormalizeConcurrency 补全并发配置的默认值：
// 0（未配置）→ 默认值，负数保持原样（不限流）。
//
// **翻译的 QPS / Burst / Concurrency 不在这里补默认值**，只做负数归一：
// 这两个值依赖当前 provider（alimt 是 QPS 50，qwenmt 只有 1 RPS），
// 而 config 包不认识 translator 包的语义。补默认值的职责交给
// translator.Config.resolveLimits——见 ResolveTranslate* 三个方法。
func (c *Config) NormalizeConcurrency() {
	if c.EnrichConcurrency == 0 {
		c.EnrichConcurrency = DefaultEnrichConcurrency
	}
	if c.SummaryConcurrency == 0 {
		c.SummaryConcurrency = DefaultAPIConcurrency
	}
	if c.TranslateQPS < 0 {
		c.TranslateQPS = 0 // 0 在下游表示"用 provider 默认值"，所以不限速要用 -1 表达
		c.TranslateBurst = 0
		c.translateUnlimited = true
	}
}

// ResolveTranslateProvider 返回归一后的翻译 provider 名称（空值 → alimt）。
func (c *Config) ResolveTranslateProvider() string {
	p, ok := translator.NormalizeProvider(c.TranslateProvider)
	if !ok {
		return strings.TrimSpace(c.TranslateProvider) // 交给 Validate 报错
	}
	return string(p)
}

// TranslateUnlimited 表示显式关闭了翻译限速（TRANSLATE_QPS 设为负数）。
func (c *Config) TranslateUnlimited() bool { return c.translateUnlimited }

// ResolveTranslateQPS 返回翻译限速的配置值：
//
//	0   表示"用该 provider 的默认值"（交由 translator 包按 provider 决定）
//	负数在 NormalizeConcurrency 里已被归一成 0 + translateUnlimited
func (c *Config) ResolveTranslateQPS() float64 {
	if c.translateUnlimited {
		return -1 // 负值 = 不限速，透传给 translator.Config.QPS
	}
	return c.TranslateQPS
}

// ResolveTranslateBurst 返回翻译的桶容量；0 表示"用 provider 默认值"。
func (c *Config) ResolveTranslateBurst() int {
	if c.translateUnlimited {
		return 0
	}
	return c.TranslateBurst
}

// ResolveTranslateConcurrency 返回翻译并发上限；0 表示"用 provider 默认值"。
func (c *Config) ResolveTranslateConcurrency() int { return c.TranslationConcurrency }

// ValidateTranslateProvider 校验 TRANSLATE_PROVIDER 的取值是否受支持。
func (c *Config) ValidateTranslateProvider() error {
	if _, ok := translator.NormalizeProvider(c.TranslateProvider); !ok {
		return fmt.Errorf("TRANSLATE_PROVIDER=%q 不受支持，可选：%s / %s（留空表示 %s）",
			c.TranslateProvider, translator.ProviderAliyunMT, translator.ProviderQwenMT, translator.ProviderAliyunMT)
	}
	return nil
}

// ResolveSummaryModel 解析生成文献介绍实际使用的模型与 key。
//
// 优先级：SUMMARY_MODEL_* > MODEL_NAME / MODEL_API_KEY。
// 两者都可能在 .env 里出现（后端自己用 MODEL_NAME 指它的对话模型），
// 所以这里只认 SUMMARY_MODEL_* 这对专属键，缺失时才回退。
func (c *Config) ResolveSummaryModel() (name, apiKey string) {
	name = strings.TrimSpace(c.SummaryModelName)
	apiKey = strings.TrimSpace(c.SummaryModelAPIKey)

	if name == "" {
		name = strings.TrimSpace(c.ModelName)
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(c.ModelAPIKey)
	}
	return name, apiKey
}

// HasSummaryModel 判断是否配置了生成文献介绍所需的模型与 key。
// 摘要生成是可降级能力（缺配置时 summary 回退为摘要原文），调用方用它决定是否装配。
func (c *Config) HasSummaryModel() bool {
	name, apiKey := c.ResolveSummaryModel()
	return name != "" && apiKey != ""
}

// Load 从 path 指向的 .env 文件读取配置。
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件 %s 失败: %w", path, err)
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件 %s 失败: %w", path, err)
	}
	cfg.NormalizeConcurrency()
	return cfg, nil
}

// ValidateForStore 校验写入 MySQL / Milvus 所需的配置，缺项时给出可操作的提示。
func (c *Config) ValidateForStore() error {
	missing := make([]string, 0, 4)
	if strings.TrimSpace(c.MysqlDSN) == "" {
		missing = append(missing, "MYSQL_DSN")
	}
	if strings.TrimSpace(c.MilvusAddress) == "" {
		missing = append(missing, "MILVUS_ADDRESS")
	}
	if strings.TrimSpace(c.MilvusCollectionName) == "" {
		missing = append(missing, "MILVUS_COLLECTION_NAME")
	}
	if c.VecDim <= 0 {
		missing = append(missing, "VEC_DIM")
	}
	if len(missing) > 0 {
		return fmt.Errorf("配置缺少必填项: %s", strings.Join(missing, ", "))
	}
	return nil
}

// ValidateForEmbedding 校验向量化所需的配置。
func (c *Config) ValidateForEmbedding() error {
	missing := make([]string, 0, 2)
	if strings.TrimSpace(c.EmbeddingModelAPIKey) == "" {
		missing = append(missing, "EMBEDDING_MODEL_API_KEY")
	}
	if strings.TrimSpace(c.EmbeddingModelBaseURL) == "" {
		missing = append(missing, "EMBEDDING_MODEL_BASE_URL_WITH_OPENAI")
	}
	if len(missing) > 0 {
		return fmt.Errorf("配置缺少必填项: %s", strings.Join(missing, ", "))
	}
	return nil
}

// HasTranslationCredentials 判断是否配置了机器翻译所需的 AccessKey。
// 翻译是可降级能力（缺 key 时中文字段留空），所以调用方用它决定是否装配。
func (c *Config) HasTranslationCredentials() bool {
	return strings.TrimSpace(c.AliyunMTAccessKeyID) != "" && strings.TrimSpace(c.AliyunMTAccessKeySecret) != ""
}

// ValidateForTranslation 校验当前 provider 的翻译凭据。
//
// alimt 的两个 AccessKey 字段只配一半几乎一定是漏填（新建 RAM 用户时容易只复制了 ID）：
// 签名需要两个值，缺一个只会得到含糊的签名错误，不如在装配阶段就说清楚。
// 两个都不配属于"主动关闭翻译"，不算错误。
//
// qwenmt 只需要一个 ApiKey，但**选中了它就必须有 key**：
// 明确指定 provider 却调不动属于配置错误，不该被当成"没配翻译"静默跳过。
func (c *Config) ValidateForTranslation() error {
	provider, ok := translator.NormalizeProvider(c.TranslateProvider)
	if !ok {
		provider = translator.ProviderAliyunMT // provider 名非法由 ValidateTranslateProvider 报错
	}

	if provider == translator.ProviderQwenMT {
		if strings.TrimSpace(c.QwenMTApiKey) == "" {
			return fmt.Errorf("TRANSLATE_PROVIDER=qwenmt 需要 QWEN_MT_API_KEY（百炼 DashScope 的 ApiKey）")
		}
		return nil
	}

	idMissing := strings.TrimSpace(c.AliyunMTAccessKeyID) == ""
	secretMissing := strings.TrimSpace(c.AliyunMTAccessKeySecret) == ""
	if idMissing == secretMissing {
		return nil // 都配了（正常）或都没配（主动关闭翻译）
	}

	configured := "ALIYUN_MT_ACCESS_KEY_ID"
	if secretMissing {
		configured = "ALIYUN_MT_ACCESS_KEY_SECRET"
	}
	return fmt.Errorf("机器翻译凭据不完整：ALIYUN_MT_ACCESS_KEY_ID 与 ALIYUN_MT_ACCESS_KEY_SECRET 必须同时配置（当前只配了 %s）", configured)
}
