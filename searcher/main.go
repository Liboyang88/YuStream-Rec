// 命令 searcher（arxiv-ingest）：抓取指定日期提交的 arXiv 论文，生成文献介绍与
// 中文翻译后写入 MySQL + Milvus。与 litstream 后端不共享代码，但复用同一个 .env，
// 保证写入同一套存储。
//
// 用法：
//
//	go run . -date 2024-06-01                    # 抓取当天全部论文并入库（自动分页）
//	go run . -date 2024-06-01 -max 200           # 最多抓 200 篇
//	go run . -date 2024-06-01 -concurrency 16    # 富化阶段同时处理 16 篇（覆盖 .env）
//	go run . -date 2024-06-01 -dry-run           # 只抓取并打印，不调用 LLM、不写库
//	go run . -date 2024-06-01 -config ../backend/.env
//
// 日期范围（含首尾，按天依次收集，每天独立去重/富化/入库）：
//
//	go run . -from 2024-06-20 -to 2025-01-20              # 覆盖这一年里每一天
//	go run . -from 2024-06-20 -to 2024-06-30 -max 50      # 每天最多抓 50 篇
//
// 维护操作（不抓取，直接操作已有数据）：
//
//	go run . -op stats                           # 查看总共有多少条 item 记录（MySQL + Milvus）
//	go run . -op dates                           # 查看记录覆盖了哪些日期
//	go run . -op delete-date -date 2024-06-20 -yes   # 删除某天的全部记录（不加 -yes 只报告规模）
//	go run . -op purge -yes                      # 清空全部记录（不加 -yes 只报告规模，不删除）
//
// 按日期重新生成（模型换代后刷新已有数据；都不加 -yes 时只报告规模）：
//
//	go run . -op regen-translate -date 2024-06-20 -yes   # 重新生成中文翻译
//	go run . -op regen-summary   -date 2024-06-20 -yes   # 重新生成文献介绍
//	go run . -op regen-embedding -date 2024-06-20 -yes   # 重新生成向量（Milvus）
//
// 注意顺序：向量是对 summary 编码得到的，换了摘要模型后要先 regen-summary 再
// regen-embedding。
//
// 注意：arXiv 的 submittedDate 按美东时间（ET）计，"当天"指 ET 的当天。
//
// 代码结构（自下而上）：
//
//	config/    读取 .env
//	domain/    入库实体（与后端 items 表一致）
//	arxiv/     arXiv API 客户端
//	llm/       chat 客户端 + 文献介绍生成（qwen3.8-flash）
//	translator/阿里云机器翻译通用版（alimt TranslateGeneral）
//	embedder/  文本向量化
//	lang/      语种判断
//	retry/     通用重试（带退避）
//	semaphore/ 并发闸门（限制同时在飞的接口请求数）
//	store/     MySQL + Milvus 读写（去重查询、统计、清空、按日期重新生成的回写）
//	storefields/ 重新生成要回写的字段集合（store 与 regen 共用）
//	regen/     按日期重新生成编排：翻译 / 摘要 / 向量
//	ingest/    抓取入库编排：抓取 → 去重 → 富化（并发）→ 向量化 → 落库
//	maintain/  维护操作编排：stats / dates / purge
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"searcher/arxiv"
	"searcher/config"
	"searcher/embedder"
	"searcher/ingest"
	"searcher/llm"
	"searcher/maintain"
	"searcher/regen"
	"searcher/semaphore"
	"searcher/store"
	"searcher/translator"
)

// -op 的取值（maintain/regen 的那些与各自包里的 Op 对应，ingest 是本脚本的默认操作）。
const (
	opIngest         = "ingest"
	opRegenTranslate = "regen-translate"
	opRegenSummary   = "regen-summary"
	opRegenEmbedding = "regen-embedding"
)

// regenOps 把 -op 取值映射到 regen.Op；不在表里就不是重新生成类操作。
var regenOps = map[string]regen.Op{
	opRegenTranslate: regen.OpTranslate,
	opRegenSummary:   regen.OpSummary,
	opRegenEmbedding: regen.OpEmbedding,
}

// opHelp 是 -op 的帮助文本与错误提示里列出的全部取值。
func opHelp() string {
	return fmt.Sprintf("%s / %s / %s / %s / %s / %s / %s / %s",
		opIngest, maintain.OpStats, maintain.OpDates, maintain.OpDeleteDate, maintain.OpPurge,
		opRegenTranslate, opRegenSummary, opRegenEmbedding)
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("%v", err)
	}
}

func run() error {
	dateStr := flag.String("date", "", "要处理的日期 YYYY-MM-DD（-op ingest 单日 / delete-date 要删的日期 / regen-* 要重新生成的日期）")
	fromStr := flag.String("from", "", "日期范围的起始日 YYYY-MM-DD（与 -to 一起用时按天依次收集入库）")
	toStr := flag.String("to", "", "日期范围的结束日 YYYY-MM-DD（含当天）")
	maxResults := flag.Int("max", 0, "最多抓取论文数；0 表示抓取当天全部（自动分页）")
	configPath := flag.String("config", "../backend/.env", "litstream 的 .env 配置文件路径")
	dryRun := flag.Bool("dry-run", false, "只抓取并打印标识与基本信息，不调用 LLM、不写库")
	concurrency := flag.Int("concurrency", 0, "富化 / 重新生成时同时处理的论文数；0 表示用 .env 或默认 8")
	op := flag.String("op", opIngest, "操作类型："+opHelp())
	confirmed := flag.Bool("yes", false, "delete-date / purge / regen-* 的确认开关：不加则只报告将要处理的规模，不执行")
	flag.Parse()
	concurrencySet := isFlagSet("concurrency")

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("加载配置失败（检查 -config 路径）: %w", err)
	}

	// -concurrency 显式给出时优先级最高，覆盖 .env；未给出则用 .env（缺失时已回退默认值）。
	if concurrencySet {
		cfg.EnrichConcurrency = *concurrency
	}

	// Ctrl-C 时取消正在进行的请求与分页等待。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch *op {
	case opIngest:
		return runIngest(ctx, cfg, *dateStr, *fromStr, *toStr, *maxResults, *dryRun)
	case string(maintain.OpStats), string(maintain.OpDates), string(maintain.OpDeleteDate), string(maintain.OpPurge):
		return runMaintain(ctx, cfg, maintain.Op(*op), *dateStr, *confirmed)
	default:
		if regenOp, ok := regenOps[*op]; ok {
			return runRegen(ctx, cfg, regenOp, *dateStr, *confirmed, *dryRun)
		}
		return fmt.Errorf("未知 -op %q，可选：%s", *op, opHelp())
	}
}

// isFlagSet 判断某个命令行参数是否被显式指定过。
// 用于区分"用户传了 -concurrency 0（想要默认值）"和"没传这个参数（用 .env 的值）"。
func isFlagSet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// runIngest 抓取论文并写入 MySQL + Milvus。
//
// 两种模式：
//   - 单日：-date 2024-06-01
//   - 日期范围：-from 2024-06-20 -to 2025-01-20（含首尾，按天依次收集，每天一批独立入库）
//
// 两种模式共用同一个 Runner 与同一条流水线，区别只在"要跑哪些日期"。
func runIngest(ctx context.Context, cfg *config.Config, dateStr, fromStr, toStr string, maxResults int, dryRun bool) error {
	dates, err := ingestDates(dateStr, fromStr, toStr)
	if err != nil {
		return err
	}

	runner, closer, err := buildRunner(ctx, cfg, dryRun)
	if err != nil {
		return err
	}
	if closer != nil {
		defer func() {
			if err := closer.Close(); err != nil {
				log.Printf("关闭连接失败: %v", err)
			}
		}()
	}

	if len(dates) == 1 {
		return runner.Run(ctx, ingest.Request{
			Date:   dates[0],
			Max:    maxResults,
			DryRun: dryRun,
		})
	}
	return runDateRange(ctx, runner, dates, maxResults, dryRun)
}

// ingestDates 解析出本次要处理的日期列表（升序，范围含首尾）。
//
// 参数校验集中在入口处：日期拼错或范围反了应当立即报错，而不是跑到一半才发现
// （范围模式下前面的日期已经入库，中途才失败会留下半截数据）。
func ingestDates(dateStr, fromStr, toStr string) ([]time.Time, error) {
	dateStr, fromStr, toStr = strings.TrimSpace(dateStr), strings.TrimSpace(fromStr), strings.TrimSpace(toStr)

	switch {
	case fromStr != "" || toStr != "":
		if dateStr != "" {
			return nil, fmt.Errorf("-date 与 -from/-to 不能同时使用：单日抓取用 -date，范围抓取用 -from + -to")
		}
		if fromStr == "" || toStr == "" {
			return nil, fmt.Errorf("范围抓取必须同时提供 -from 与 -to，例如 -from 2024-06-20 -to 2025-01-20")
		}
		from, err := parseDate("-from", fromStr)
		if err != nil {
			return nil, err
		}
		to, err := parseDate("-to", toStr)
		if err != nil {
			return nil, err
		}
		if to.Before(from) {
			return nil, fmt.Errorf("-to (%s) 不能早于 -from (%s)", toStr, fromStr)
		}
		return dateRange(from, to), nil

	case dateStr != "":
		date, err := parseDate("-date", dateStr)
		if err != nil {
			return nil, err
		}
		return []time.Time{date}, nil

	default:
		return nil, fmt.Errorf("必须提供 -date（单日），或 -from 与 -to（日期范围）")
	}
}

// parseDate 按 YYYY-MM-DD 解析日期。
func parseDate(flagName, value string) (time.Time, error) {
	date, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s 日期格式错误，应为 YYYY-MM-DD: %q", flagName, value)
	}
	return date, nil
}

// dateRange 返回 [from, to] 之间（含首尾）的每一天，按时间升序。
func dateRange(from, to time.Time) []time.Time {
	total := int(to.Sub(from).Hours()/24) + 1
	dates := make([]time.Time, 0, total)
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		dates = append(dates, d)
	}
	return dates
}

// rangeResult 是某一天的执行结果（只用于汇总统计）。
type rangeResult struct {
	Date time.Time
	Err  error // 该天的失败原因；nil 表示成功
}

// runDateRange 逐天执行抓取入库，并在结束时打印汇总。
//
// 逐天串行而不是并行：一是每天本来就是独立的一批（去重、富化、写库都有自己的节奏），
// 二是并行跑多天会让 arXiv 与 LLM 的瞬时压力叠加，反而更容易被限流。
// 天数较多时真正耗时的是富化，而富化在单天内部已经是并发的。
//
// 某一天失败不会中断整体：已成功的那些天已经入库，继续跑完剩下的日期才能拿到
// 尽可能完整的数据；失败日期会在最后的汇总里列出，重跑时已入库的部分会被去重跳过。
func runDateRange(ctx context.Context, runner *ingest.Runner, dates []time.Time, maxResults int, dryRun bool) error {
	log.Printf("按日期范围收集：%s ~ %s 共 %d 天，逐天入库",
		ingest.FormatDate(dates[0]), ingest.FormatDate(dates[len(dates)-1]), len(dates))

	results := make([]rangeResult, 0, len(dates))
	for i, date := range dates {
		if err := ctx.Err(); err != nil {
			// Ctrl-C：停止调度剩余日期，但在返回前把已完成的进度报出来。
			log.Printf("已中断，剩余 %d 天未处理: %v", len(dates)-i, err)
			break
		}

		dateText := ingest.FormatDate(date)
		log.Printf("[%d/%d] %s 开始...", i+1, len(dates), dateText)
		err := runner.Run(ctx, ingest.Request{Date: date, Max: maxResults, DryRun: dryRun})
		if err != nil {
			// 目标日期内失败不打断整轮：记下来继续跑下一天。
			log.Printf("[%d/%d] %s 失败: %v", i+1, len(dates), dateText, err)
			fmt.Printf("[%d/%d] %s ✗ %v\n", i+1, len(dates), dateText, err)
		} else {
			fmt.Printf("[%d/%d] %s ✓\n", i+1, len(dates), dateText)
		}
		results = append(results, rangeResult{Date: date, Err: err})
	}

	printRangeSummary(results, dates, ctx.Err())

	failed := failedDates(results)
	if len(failed) > 0 {
		return fmt.Errorf("有 %d 天失败：%s（重跑同样的 -from/-to 即可补齐，已入库的论文会被去重跳过）",
			len(failed), strings.Join(failed, ", "))
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

// failedDates 汇总失败的日期（YYYY-MM-DD 列表）。
func failedDates(results []rangeResult) []string {
	var failed []string
	for _, r := range results {
		if r.Err != nil {
			failed = append(failed, ingest.FormatDate(r.Date))
		}
	}
	return failed
}

// printRangeSummary 打印范围收集的汇总：成功/失败天数与失败日期，便于直接补跑。
func printRangeSummary(results []rangeResult, dates []time.Time, interrupted error) {
	succeeded, failed := 0, 0
	for _, r := range results {
		if r.Err != nil {
			failed++
			continue
		}
		succeeded++
	}

	fmt.Printf("\n===== 日期范围收集汇总 =====\n")
	fmt.Printf("计划 %d 天，已完成 %d 天：成功 %d 天，失败 %d 天\n", len(dates), len(results), succeeded, failed)

	if list := failedDates(results); len(list) > 0 {
		fmt.Printf("失败日期：%s\n", strings.Join(list, ", "))
	}
	if interrupted != nil {
		remaining := len(dates) - len(results)
		fmt.Printf("被中断：%v（剩余 %d 天未处理）\n", interrupted, remaining)
	}
	if skipped := len(dates) - len(results); skipped > 0 && interrupted == nil {
		fmt.Printf("未处理 %d 天\n", skipped)
	}
	fmt.Printf("============================\n")
}

// runMaintain 执行不涉及抓取的维护操作（stats / dates / delete-date / purge）。
//
// 一律不在连接阶段创建 Milvus 集合：只读命令不该有副作用，删除被拒绝时也不该留下
// 痕迹。真正需要集合可用时由 PurgeAll 自己在删完之后重建（空但可用）；
// delete-date 则只删主键，不碰集合本身。
func runMaintain(ctx context.Context, cfg *config.Config, op maintain.Op, date string, confirmed bool) error {
	if err := cfg.ValidateForStore(); err != nil {
		return err
	}

	st, err := store.Open(ctx, store.Options{
		DSN:                cfg.MysqlDSN,
		MilvusAddress:      cfg.MilvusAddress,
		Collection:         cfg.MilvusCollectionName,
		VecDim:             cfg.VecDim,
		SkipCollectionInit: true,
	})
	if err != nil {
		return err
	}
	defer func() {
		if err := st.Close(); err != nil {
			log.Printf("关闭连接失败: %v", err)
		}
	}()

	runner, err := maintain.NewRunner(st)
	if err != nil {
		return err
	}
	return runner.Run(ctx, maintain.Request{Op: op, Date: date, Confirmed: confirmed})
}

// runRegen 按日期重新生成派生字段（regen-translate / regen-summary / regen-embedding）。
//
// 与 runMaintain 的区别是它需要外部能力（翻译 / 摘要 / embedding），因此只在对应
// 操作所需的凭据齐备时才去连库；集合不在这里创建——三个操作里只有 embedding 需要
// 集合可用，由 store.ReplaceVectors 自己按需创建并加载。
//
// dry-run 只报告该日期有多少条记录，不调用任何接口、不写库。
func runRegen(ctx context.Context, cfg *config.Config, op regen.Op, date string, confirmed, dryRun bool) error {
	// 先校验命令行参数：-date 打错了就该立刻报出来，而不是等连完库、
	// 校验完凭据才提示（那会让人误以为是配置问题）。
	if strings.TrimSpace(date) == "" {
		return fmt.Errorf("-op regen-%s 必须提供 -date（YYYY-MM-DD），例如 -op regen-%s -date 2024-06-20", op, op)
	}

	if err := cfg.ValidateForStore(); err != nil {
		return err
	}
	if err := cfg.ValidateTranslateProvider(); err != nil {
		return err
	}

	// 只有重建翻译时才要求翻译凭据：regen-summary / regen-embedding 与翻译无关，
	// 不该因为它们而卡在"没配 QWEN_MT_API_KEY"上。
	// 翻译凭据"只配一半"属于漏填，直接报错（与 ingest 一致）。
	if op == regen.OpTranslate {
		if err := cfg.ValidateForTranslation(); err != nil {
			return err
		}
	}

	// dry-run 只报规模：既不调用接口，也不必要求对应能力的 key。
	if dryRun {
		confirmed = false
		log.Printf("dry-run：只报告 %s 要处理的记录数，不调用接口、不写库", opDescription(op))
	} else if op == regen.OpEmbedding {
		if err := cfg.ValidateForEmbedding(); err != nil {
			return err
		}
	}

	deps := regen.Deps{
		Translator:  newTranslator(cfg),
		Summarizer:  newSummarizer(cfg),
		Concurrency: cfg.EnrichConcurrency,
	}

	st, err := store.Open(ctx, store.Options{
		DSN:                cfg.MysqlDSN,
		MilvusAddress:      cfg.MilvusAddress,
		Collection:         cfg.MilvusCollectionName,
		VecDim:             cfg.VecDim,
		SkipCollectionInit: op != regen.OpEmbedding,
	})
	if err != nil {
		return err
	}
	defer func() {
		if err := st.Close(); err != nil {
			log.Printf("关闭连接失败: %v", err)
		}
	}()

	deps.Repository = st
	if op == regen.OpEmbedding {
		deps.Vectorizer = embedder.New(cfg.EmbeddingModelAPIKey, cfg.EmbeddingModelBaseURL, cfg.EmbeddingModelName, cfg.VecDim)
		log.Printf("embedding 模型=%s，维度=%d", cfg.EmbeddingModelName, cfg.VecDim)
	}

	runner, err := regen.NewRunner(deps)
	if err != nil {
		return err
	}
	log.Printf("按日期重新生成 %s，条目级并发 %d", opDescription(op), runner.Concurrency())
	return runner.Run(ctx, regen.Request{Op: op, Date: date, Confirmed: confirmed})
}

// opDescription 返回 regen 操作的中文说明（用于日志）。
func opDescription(op regen.Op) string {
	switch op {
	case regen.OpTranslate:
		return "中文翻译（title_chinese / abstract_chinese）"
	case regen.OpSummary:
		return "文献介绍（summary）"
	case regen.OpEmbedding:
		return "向量（Milvus 向量）"
	default:
		return string(op)
	}
}

// buildRunner 装配依赖。dry-run 只需要抓取能力，因此不连接存储、不做配置校验。
// 第二个返回值是需要关闭的资源（非 dry-run 时为 Store），没有则为 nil。
func buildRunner(ctx context.Context, cfg *config.Config, dryRun bool) (*ingest.Runner, io.Closer, error) {
	// 翻译凭据"只配一半"几乎一定是漏填，这里直接报错而不是静默跳过翻译；
	// dry-run 不调用外部接口，所以只在真正富化前校验。
	if !dryRun {
		if err := cfg.ValidateForTranslation(); err != nil {
			return nil, nil, err
		}
	}

	deps := ingest.Deps{
		Source:            arxiv.NewClient(),
		Summarizer:        newSummarizer(cfg),
		Translator:        newTranslator(cfg),
		EnrichConcurrency: cfg.EnrichConcurrency,
	}

	if dryRun {
		runner, err := ingest.NewRunner(deps)
		return runner, nil, err
	}

	if err := cfg.ValidateForStore(); err != nil {
		return nil, nil, err
	}
	if err := cfg.ValidateForEmbedding(); err != nil {
		return nil, nil, err
	}

	st, err := store.Open(ctx, store.Options{
		DSN:           cfg.MysqlDSN,
		MilvusAddress: cfg.MilvusAddress,
		Collection:    cfg.MilvusCollectionName,
		VecDim:        cfg.VecDim,
	})
	if err != nil {
		return nil, nil, err
	}

	deps.Repository = st
	deps.Vectorizer = embedder.New(cfg.EmbeddingModelAPIKey, cfg.EmbeddingModelBaseURL, cfg.EmbeddingModelName, cfg.VecDim)

	runner, err := ingest.NewRunner(deps)
	if err != nil {
		st.Close()
		return nil, nil, err
	}
	return runner, st, nil
}

// newSummarizer 在缺少配置时返回 nil，由 ingest 侧降级为"用摘要原文当 summary"。
//
// 模型名与 key 由 cfg.ResolveSummaryModel 解析：优先 SUMMARY_MODEL_*，
// 缺失时回退到 MODEL_NAME / MODEL_API_KEY。
//
// summary 与翻译是两个独立服务、两套额度：summary 受 TPM 约束，翻译受 QPS 约束，
// 所以并发闸门仍然是分开的。
func newSummarizer(cfg *config.Config) ingest.Summarizer {
	modelName, apiKey := cfg.ResolveSummaryModel()
	if modelName == "" || apiKey == "" {
		log.Print("警告：未配置 SUMMARY_MODEL_NAME / SUMMARY_MODEL_API_KEY（也无 MODEL_NAME / MODEL_API_KEY 可回退），跳过文献介绍生成（summary 回退为摘要原文）")
		return nil
	}
	return llm.NewSummarizer(apiKey, modelName,
		llm.WithSemaphore(semaphore.New(cfg.SummaryConcurrency)))
}

// newTranslator 按 TRANSLATE_PROVIDER 装配翻译客户端。
//
// 两种实现可选（见 translator 包）：
//
//	alimt   阿里云机器翻译通用版 —— 用 RAM 的 AccessKey 签名，额度 QPS 50，
//	        默认放开跑，适合大批量历史数据入库；
//	qwenmt  百炼 Qwen-MT —— 用百炼 ApiKey，额度只有 60 RPM（≈1 RPS），
//	        因此默认限成"每秒 1 个请求 + 低并发"（见 translator.DefaultQwen*）。
//
// 选择某个 provider 却没配对应凭据时**直接报错**而不是静默跳过：
// 明确指定了 provider 却调不动属于配置错误，静默跳过会让人以为翻译在跑。
// 完全没配任何翻译凭据（且没指定 provider）才降级为跳过翻译。
func newTranslator(cfg *config.Config) ingest.Translator {
	if err := cfg.ValidateTranslateProvider(); err != nil {
		// 早退：provider 名写错时不该悄悄退回默认实现。
		log.Printf("翻译配置错误: %v", err)
		return nil
	}

	provider := cfg.ResolveTranslateProvider()
	// 用户显式选了 qwenmt，就要求它有 key；否则按 alimt 的凭据是否存在决定。
	explicitQwen := provider == string(translator.ProviderQwenMT)

	if explicitQwen {
		if strings.TrimSpace(cfg.QwenMTApiKey) == "" {
			log.Print("警告：TRANSLATE_PROVIDER=qwenmt 但未配置 QWEN_MT_API_KEY，跳过中文翻译")
			return nil
		}
	} else if !cfg.HasTranslationCredentials() {
		log.Print("警告：未配置 ALIYUN_MT_ACCESS_KEY_ID / ALIYUN_MT_ACCESS_KEY_SECRET，跳过中文翻译")
		return nil
	}

	if err := cfg.ValidateForTranslation(); err != nil {
		log.Printf("翻译配置错误: %v", err)
		return nil
	}

	client, err := translator.New(translator.Config{
		Provider:        translator.Provider(provider),
		AccessKeyID:     cfg.AliyunMTAccessKeyID,
		AccessKeySecret: cfg.AliyunMTAccessKeySecret,
		APIKey:          cfg.QwenMTApiKey,
		Model:           cfg.QwenMTModel,
		QPS:             cfg.ResolveTranslateQPS(),
		Burst:           cfg.ResolveTranslateBurst(),
		Concurrency:     cfg.ResolveTranslateConcurrency(),
	})
	if err != nil {
		log.Printf("装配翻译客户端失败: %v", err)
		return nil
	}

	log.Printf("翻译 provider=%s（限速与并发按该 provider 的额度自动取值）", provider)
	return client
}
