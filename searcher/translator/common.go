// Package translator 提供两种机器翻译实现，可按配置切换：
//
//	alimt    阿里云机器翻译通用版（alimt TranslateGeneral，RAM AccessKey 签名）
//	qwenmt   阿里云百炼 Qwen-MT（OpenAI 兼容接口，DashScope ApiKey）
//
// 两者对外都实现 Client 接口，调用方（ingest / regen）只依赖该接口，
// 装配时用 New 按 Provider 选择具体实现。
//
// 为什么保留两种：
//
//   - alimt 的额度是 **QPS 50**，吞吐高，适合大批量历史数据入库；
//   - qwen-mt-lite 的额度只有 **60 RPM（≈1 RPS）**，但对术语与长句的处理更好，
//     适合小批量重译或对译文质量要求高的场景。
//
// 两者的限流口径相差两个数量级，因此**并发与限速按 provider 分别默认**，
// 详见 New：alimt 默认放开到 QPS 50，qwenmt 默认就是"每秒 1 个请求 + 低并发"。
//
// 本文件放两种实现共用的部分：接口、语言常量、合并/拆回、降级、术语表。
package translator

import (
	"context"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"searcher/retry"
	"searcher/semaphore"
)

// Provider 是翻译实现的类型。
type Provider string

const (
	// ProviderAliyunMT 是阿里云机器翻译通用版（默认）。
	ProviderAliyunMT Provider = "alimt"
	// ProviderQwenMT 是阿里云百炼 Qwen-MT。
	ProviderQwenMT Provider = "qwenmt"
)

// Providers 返回全部受支持的 provider（用于错误提示与校验）。
func Providers() []Provider { return []Provider{ProviderAliyunMT, ProviderQwenMT} }

// NormalizeProvider 把配置里的字符串归一成 Provider。
// 空值、以及常见写法（aliyunmt / qwen-mt / qwen / dashscope）都接受，
// 避免因为大小写或连字符写错而启动失败。
func NormalizeProvider(s string) (Provider, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "alimt", "aliyunmt", "aliyun_mt", "aliyun-mt", "mt", "general":
		return ProviderAliyunMT, true
	case "qwenmt", "qwen_mt", "qwen-mt", "qwen", "bailian", "dashscope":
		return ProviderQwenMT, true
	default:
		return "", false
	}
}

// pairSeparator 用于把标题与摘要拼成一次请求，再按同一分隔符拆回，
// 从而把两次翻译合并为一次调用。
//
// 两种实现共用同一个分隔符：拆回逻辑（splitPair）对"引擎把分隔符加了空格"
// 已经做了容错，见 findSeparatorSpan。
const pairSeparator = "|||SPLIT_MT|||"

// requestTimeout 是单次翻译请求的超时（分段后单次请求的文本不会太长）。
const requestTimeout = 60 * time.Second

// LangAuto / LangChinese / LangEnglish 是语言标识，保留全称形式（历史调用点沿用）。
//
// 两种实现用的语言码不同（alimt 是短码 zh/en，Qwen-MT 是全称 Chinese/English），
// 因此各自由 normalizeLang 转成所需形态。
const (
	LangAuto    = "auto"
	LangChinese = "Chinese"
	LangEnglish = "English"
)

// Client 是一次翻译会话。
//
// 两种 provider 都实现它，调用方只依赖这个接口：
// 批量入库（ingest）与按日期重新生成（regen）都不需要知道底层用的是哪一种。
type Client interface {
	// Translate 翻译单段文本。srcLang 可传 LangAuto 让接口自动识别源语言。
	Translate(ctx context.Context, text, srcLang, dstLang string) (string, error)
	// TranslateItem 把论文标题与摘要翻译成中文。
	TranslateItem(ctx context.Context, title, abstract string) (titleZh, abstractZh string, err error)
}

// GlossaryItem 术语条目：指定术语的固定译法。
//
// alimt 没有原生的术语表参数，由客户端用占位符实现（见 maskGlossary）；
// Qwen-MT 有原生 translation_options.glossary，但两者都走同一份客户端实现，
// 保证切换 provider 时术语行为一致。
type GlossaryItem struct {
	Term        string `json:"term"`
	Translation string `json:"translation"`
}

// defaultGlossary 是默认术语表，保证领域内的惯用译法一致。
var defaultGlossary = []GlossaryItem{
	{Term: "RAG", Translation: "检索增强生成"},
}

// options 是两种实现共用的可选项。New 里由各 provider 补默认值后交给具体实现。
type options struct {
	httpClient *http.Client
	endpoint   string
	// model 只对 Qwen-MT 有意义（alimt 的模型由 Action/Version 决定，没有模型名参数）。
	model    string
	glossary []GlossaryItem
	retry    retry.Config
	gate     *semaphore.Semaphore
	limiter  *semaphore.Limiter
	// maxTextLength 只对 alimt 有意义（单次请求 5000 字符上限，超长要分段）。
	maxTextLength int
	// now / nonce 只对 alimt 的签名有意义；便于测试替换。
	now   func() time.Time
	nonce func() string
}

// newOptions 返回带默认值的可选项。
func newOptions() options {
	return options{
		httpClient:    &http.Client{Timeout: requestTimeout},
		glossary:      defaultGlossary,
		retry:         retry.DefaultConfig(),
		maxTextLength: maxTextLengthDefault,
	}
}

// ---- 共用：标题 + 摘要的合并与拆回 ----

// translateItem 是 TranslateItem 的共用实现。
//
// 标题与摘要都非空时合并成一次请求（省掉一半调用），再按分隔符拆回；
// 若接口没有原样保留分隔符导致拆分失败，则降级为标题、摘要分别翻译——
// 多花一次调用，但比整条中文字段丢掉要好。
// 摘要为空时只翻译标题。
//
// segments 由各实现提供：alimt 需要按 5000 字符上限预先分段，
// Qwen-MT 没有这个限制（整段发，分段函数返回单个元素）。
func translateItem(ctx context.Context, c Client, title, abstract string, segments func(string) []string) (titleZh, abstractZh string, err error) {
	if strings.TrimSpace(abstract) == "" {
		titleZh, err = c.Translate(ctx, title, LangAuto, LangChinese)
		return strings.TrimSpace(titleZh), "", err
	}

	translated, err := translateSegments(ctx, c, segments(title+pairSeparator+abstract), LangAuto, LangChinese)
	if err != nil {
		return "", "", err
	}

	titleZh, abstractZh, splitErr := splitPair(translated, pairSeparator)
	if splitErr == nil {
		return titleZh, abstractZh, nil
	}

	log.Printf("合并翻译的结果无法拆分，降级为标题与摘要分别翻译: %v", splitErr)
	return translateSeparately(ctx, c, title, abstract)
}

// translateSegments 逐个翻译分段，成功后再拼接。
//
// 先翻译完所有分段再拼接，而不是边翻边接：任何一段失败都要整体重试，
// 半成品译文拼出来只会变成一条语义断裂的中文字段。
//
// 拼接用空串而不是 pairSeparator：分段时已经保证"分隔符完整留在某一段里"，
// 各段译文顺序串接就等于原文的译文；再插一个分隔符反而会多出一个，
// 把 splitPair 拆出的标题与摘要整体错位。
func translateSegments(ctx context.Context, c Client, segments []string, srcLang, dstLang string) (string, error) {
	translated := make([]string, 0, len(segments))
	for _, segment := range segments {
		out, err := c.Translate(ctx, segment, srcLang, dstLang)
		if err != nil {
			return "", err
		}
		translated = append(translated, out)
	}
	return strings.Join(translated, ""), nil
}

// translateSeparately 分别翻译标题与摘要（合并结果拆分失败时的降级路径）。
// 两边都失败才返回错误；只有一边失败时返回另一边的结果，缺失的一边留空。
func translateSeparately(ctx context.Context, c Client, title, abstract string) (titleZh, abstractZh string, err error) {
	translatedTitle, titleErr := c.Translate(ctx, title, LangAuto, LangChinese)
	if titleErr != nil {
		log.Printf("标题翻译失败（留空）: %v", titleErr)
	}
	translatedAbstract, abstractErr := c.Translate(ctx, abstract, LangAuto, LangChinese)
	if abstractErr != nil {
		log.Printf("摘要翻译失败（留空）: %v", abstractErr)
	}

	if titleErr != nil && abstractErr != nil {
		return "", "", &TranslateError{Title: titleErr, Abstract: abstractErr}
	}
	return strings.TrimSpace(translatedTitle), strings.TrimSpace(translatedAbstract), nil
}

// TranslateError 表示降级路径下标题与摘要都失败。
type TranslateError struct {
	Title    error
	Abstract error
}

func (e *TranslateError) Error() string {
	return "标题与摘要分别翻译均失败: 标题=" + e.Title.Error() + "; 摘要=" + e.Abstract.Error()
}

func (e *TranslateError) Unwrap() error { return e.Abstract }

// SplitError 表示合并译文无法拆回标题与摘要。
type SplitError struct {
	Sep        string
	Translated string
	Multiple   bool
}

func (e *SplitError) Error() string {
	if e.Multiple {
		return "翻译结果里出现了多个分隔符 " + e.Sep + ": " + e.Translated
	}
	return "翻译结果无法按 " + e.Sep + " 拆分为标题与摘要: " + e.Translated
}

// splitPair 按 sep 把译文拆回标题与摘要两部分。
//
// 先试精确拆分（接口原样保留分隔符的情况）。失败时改用 findSeparatorSpan：
// 机器翻译会按中文排版习惯重排符号周围的空白，还会把连续的竖线合并。
// 实测 alimt 对 |||SPLIT_MT||| 至少给出过这几种形态：
//
//	||| SPLIT_MT |||    每个 | 后插空格（最常见）
//	| SPLIT_MT | |      开头的 ||| 被并成一个 |
//	| SPLIT_MT |        两侧都被并成一个 |
//
// 都只是排版差异，语义上是同一个分隔符。按精确匹配会把它误判成"拆分失败"，
// 于是白白走一遍"标题与摘要分别翻译"的降级重译（多花一倍调用）。
func splitPair(translated, sep string) (string, string, error) {
	if parts := strings.Split(translated, sep); len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
	}

	start, end, ok := findSeparatorSpan(translated, sep)
	if !ok {
		return "", "", &SplitError{Sep: sep, Translated: translated}
	}
	// 分隔符必须只出现一次：出现两次说明译文结构已经不是"标题+摘要"了。
	if _, _, again := findSeparatorSpan(translated[end:], sep); again {
		return "", "", &SplitError{Sep: sep, Translated: translated, Multiple: true}
	}
	return strings.TrimSpace(translated[:start]), strings.TrimSpace(translated[end:]), nil
}

// findSeparatorSpan 在 out 里找出"分隔符整体"的区间 [start, end)，找不到返回 ok=false。
//
// 实现思路是"先认核心、再向外扩"，而不是写一个正则去匹配所有变形：
// 分隔符的骨架是去掉竖线后的部分（|||SPLIT_MT||| → SPLIT_MT），先在译文里按
// 忽略大小写找到它，然后向两侧把紧邻的竖线与空白一起吃进来。
// 这样 "||| SPLIT_MT |||"、"| SPLIT_MT | |"、"| SPLIT_MT |" 都能覆盖，
// 而且不会像正则那样在"空白 + 可选竖线"的组合上出现回溯歧义。
//
// 必须真的吃到竖线才算命中：否则正文里恰好出现的骨架词（例如原文里的 SPLIT_MT）
// 会被误当成分隔符。
func findSeparatorSpan(out, sep string) (start, end int, ok bool) {
	core := strings.Trim(sep, "|")
	if core == "" {
		return 0, 0, false
	}

	for i := 0; i+len(core) <= len(out); i++ {
		if !strings.EqualFold(out[i:i+len(core)], core) {
			continue
		}

		start = i
		for start > 0 && isSeparatorNoise(out[start-1]) {
			start--
		}
		end = i + len(core)
		for end < len(out) && isSeparatorNoise(out[end]) {
			end++
		}
		if strings.Contains(out[start:end], "|") {
			return start, end, true
		}
	}
	return 0, 0, false
}

// isSeparatorNoise 判断某个字符是否属于"分隔符周围可能被插进来的填充"。
// 只认竖线与水平空白：不能吃换行，否则会把正文一起吞掉。
func isSeparatorNoise(c byte) bool {
	return c == '|' || c == ' ' || c == '\t'
}

// normalizeLang 把语言标识归一成短码（zh / en / auto）。
// 短码是 alimt 需要的形态；Qwen-MT 会在其 normalizeLang 里再转成全称。
func normalizeLang(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "", "auto", "自动", "自动识别":
		return LangAuto
	case "zh", "cn", "chinese", "中文", "简体中文":
		return "zh"
	case "en", "english", "英文", "英语":
		return "en"
	default:
		return strings.TrimSpace(lang)
	}
}

// ---- 术语表（两种 provider 共用） ----

// glossaryPlaceholder 生成第 i 个术语的占位符。
//
// 形态是纯小写字母 + 数字（zqmarkerq0qz），这是实测出来的：
// 原来的 XGLOSSARYTERMX0X 会被机器翻译当成有意义的内容处理——
// alimt 会把 "GLOSSARY" 直接译成"术语表"（返回 "X术语表X0X"），
// "MT" 也会被改动，于是还原时找不到占位符，占位符就漏进了中文字段。
//
// 现在的形态既没有空格/标点可以拆，也不像词典里的词，实测两个 provider 都能原样保留。
func glossaryPlaceholder(i int) string {
	return "zqmarkerq" + itoa(i) + "qz"
}

// itoa 是不引入 strconv 的整数转字符串（占位符下标很小，够用即可）。
func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// usableGlossary 过滤掉空条目，并按术语长度降序排列。
//
// 降序是为了先替换长术语：否则 "RAG" 会先把 "RAGAS" 里的前缀换掉，
// 留下 "检索增强生成AS" 这种残句。
func usableGlossary(glossary []GlossaryItem) []GlossaryItem {
	items := make([]GlossaryItem, 0, len(glossary))
	for _, g := range glossary {
		if strings.TrimSpace(g.Term) == "" || strings.TrimSpace(g.Translation) == "" {
			continue
		}
		items = append(items, g)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return len(items[i].Term) > len(items[j].Term)
	})
	return items
}

// maskGlossary 把原文里的术语替换成占位符，并返回"把占位符换回译法"的还原函数。
//
// 替换与还原都以占位符为唯一锚点，因此还原时只认占位符，不认引擎的译文，
// 也就不会误伤正文里恰好出现的同名单词。没有术语时原样返回原文。
//
// 还原是**忽略大小写**的：实测机器翻译会把占位符的大小写改掉
// （XGLOSSARYTERMX0X → Xglossarydermx0x），按精确匹配还原会让占位符原样留在译文里，
// 直接污染写进库的中文字段。
func maskGlossary(text string, glossary []GlossaryItem) (string, func(string) string) {
	items := usableGlossary(glossary)
	if len(items) == 0 || text == "" {
		return text, func(s string) string { return s }
	}

	masked := text
	type pair struct {
		pattern     *regexp.Regexp
		translation string
	}
	applied := make([]pair, 0, len(items))

	for i, item := range items {
		if !strings.Contains(masked, item.Term) {
			continue
		}
		placeholder := glossaryPlaceholder(i)
		masked = strings.ReplaceAll(masked, item.Term, placeholder)
		applied = append(applied, pair{
			pattern:     regexp.MustCompile(`(?i)` + regexp.QuoteMeta(placeholder)),
			translation: item.Translation,
		})
	}

	return masked, func(out string) string {
		for _, p := range applied {
			out = p.pattern.ReplaceAllLiteralString(out, p.translation)
		}
		return out
	}
}

// runeLen 返回字符数（不是字节数）：alimt 的 5000 限制按字符计。
func runeLen(s string) int { return utf8.RuneCountInString(s) }
