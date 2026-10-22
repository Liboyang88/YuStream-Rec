# searcher（arxiv-ingest）

抓取指定日期提交的 arXiv 论文，生成文献介绍（summary）与中文翻译，向量化后写入
MySQL（`items` 表）与 Milvus（后端同名集合）。本目录是独立脚本，不与 litstream 后端
共享代码，但复用后端同一份 `.env`，因此写入的是同一套存储。

## 目录结构

| 目录 | 职责 |
| --- | --- |
| `main.go` | 命令行入口：解析参数（单日 / 日期范围）、装配依赖、逐天编排 |
| `config/` | 读取后端 `.env` |
| `domain/` | `Item` 实体（与后端 `items` 表逐列对齐）与唯一标识 `Identifier()` |
| `arxiv/` | arXiv Atom API 客户端：分页、重试、IP 回退、XML 解析 |
| `llm/` | OpenAI 兼容 chat 客户端 + 文献介绍生成（千问 `qwen3.8-flash`） |
| `translator/` | 翻译：两种实现可选（alimt 机器翻译通用版 / 百炼 Qwen-MT），见下文 |
| `embedder/` | 文本向量化（与后端同一模型） |
| `lang/` | 语种判断（是否已是中文，决定是否需要翻译） |
| `retry/` | 通用重试：带退避，可标记"不可重试"的错误 |
| `semaphore/` | 并发闸门（限制同时在飞几个）+ 限流器（限制多久发一个） |
| `store/` | MySQL + Milvus 读写：去重查询、统计、清空、按日期重新生成的回写 |
| `storefields/` | 重新生成要回写的字段集合（`store` 与 `regen` 共用，避免两边各声明一份类型） |
| `regen/` | 按日期重新生成编排：翻译 / 摘要 / 向量 |
| `ingest/` | 抓取入库编排：抓取 → 去重 → 富化 → 向量化 → 落库 |
| `maintain/` | 维护操作编排：`stats` / `dates` / `purge` |

## 去重规则（重要）

**唯一标识是 `paper.ArxivID` / `item.Url`，不是标题。** arXiv ID（形如
`http://arxiv.org/abs/2406.00001v1`）写入 `items.url` 列，`domain.Item.Identifier()`
返回裁剪空白后的 `Url`。去重分两步，两步都按该标识比较：

1. **批内去重**（`ingest.KeepNew`）：同一次抓取内标识重复时只保留第一条；
2. **库内去重**（`store.ExistingIdentifiers` + `ingest.KeepNew`）：标识已存在于
   `items` 表的条目整条过滤。查询包含被软删除的行，避免同一标识重复插入新行。

去重发生在**调用 LLM / 翻译之前**：已存在的论文不再浪费摘要与翻译接口的调用。
没有标识（`url` 为空）的条目无法判定是否重复，会被保留而不是丢弃。

> 注意：`items` 表里更早版本写入的数据可能没有 `url` 值（旧结构没有该列）。
> 这些历史行无法通过标识匹配，若重新抓取它们对应的日期，会作为新数据再次插入。

## 并发加速（重要）

富化阶段（生成文献介绍 + 翻译）是纯网络等待，串行执行会把时间全花在等接口回应上：
每篇论文两次调用，500 篇就是 1000 次串行等待。因此 `ingest.EnrichAll` 改为**并发富化**：

- **条目级并发**：同时处理最多 `ENRICH_CONCURRENCY`（默认 8）篇论文；
- **单条内两步并发**：同一篇的"生成文献介绍"与"翻译"同时发出——两者调用的是**不同服务**
  （千问 MaaS / 阿里云机器翻译），互不依赖，合起来把单条的等待时间压掉近一半；
- **每个接口独立限流**：summary 与翻译各自有并发闸门（`SUMMARY_CONCURRENCY` /
  `TRANSLATE_CONCURRENCY`）。两者是不同服务、不同额度（summary 受 TPM 约束、
  翻译受 QPS 约束），所以分开控制；闸门只占用"一次网络往返"的名额，
  等待重试退避的 goroutine 不占名额。
- **翻译额外按速率放行**：并发闸门管不住"发出的节奏"——并发 16、每次 0.3s 时会在
  300ms 内连发 16 个，超过接口 QPS 就会被限流。因此翻译再叠一层限流器。
  **默认值按 provider 取**（见下一节）：alimt 是每秒 50 个，qwenmt 是每秒 1 个。

按 500 篇、单次调用 2~5s 估算，串行需要约 30~80 分钟；默认配置下（条目并发 8 + 单条两步
并发）理论上是几十倍的差距，实际会落在几分钟量级——因为接口侧的排队与限流会成为新瓶颈。
继续调大并发数收益递减，反而更容易触发限流。

> 翻译有两种实现可选（`TRANSLATE_PROVIDER`）：
>
> - `alimt`（默认）机器翻译通用版，额度 **QPS 50**，适合大批量入库；
> - `qwenmt` 百炼 Qwen-MT，额度只有 **60 RPM（≈1 RPS）**，但对术语与长句的处理更好，
>   适合小批量重译。选中它时**默认自动压到"每秒 1 个请求 + 并发 2"**，
>   也就是换用 alimt 之前那套控制并发的口径。

### 怎么调

```bash
go run . -date 2024-06-01 -concurrency 16    # 同时处理 16 篇（优先级高于 .env）
```

- `-concurrency N`：条目级并发，显式给出时覆盖 `.env` 的 `ENRICH_CONCURRENCY`；
- `.env` 的 `ENRICH_CONCURRENCY` / `SUMMARY_CONCURRENCY`：未配置时用默认值 8；
- `.env` 的 `TRANSLATE_CONCURRENCY` / `TRANSLATE_QPS` / `TRANSLATE_BURST`：
  **留空（0）表示"用当前 provider 的默认值"**，不是"不限速"：
  alimt 是 8 / 50 / 50，qwenmt 是 2 / 1 / 1。
  显式设**负数**表示不限并发 / 不限速（只在确认接口不限流时使用）。

### 翻译限速怎么定（按 provider）

两种实现的额度相差两个数量级，所以默认值必须分开取：

| provider | QPS | 并发 | 依据 |
| --- | --- | --- | --- |
| `alimt` | 50 | 8 | 机器翻译通用版文档写明的 QPS 限制 50 |
| `qwenmt` | 1 | 2 | qwen-mt-lite 只有 60 RPM（≈1 RPS），且对瞬时激增有额外保护 |

`alimt` 的桶容量与 QPS 同量级（50），是为了让富化开头同时发出的十几条翻译立刻放行，
而不是被排成整齐的一秒一串。调参经验：

| 现象 | 处理 |
| --- | --- |
| 频繁收到限流错误 | 调低 `TRANSLATE_QPS`，并同步调小 `TRANSLATE_BURST` |
| 需要更高额度 | 提交工单扩容后，把 `TRANSLATE_QPS` 提到新上限 |
| qwenmt 报 `Request rate increased too quickly` | 保持 `TRANSLATE_QPS=1`、`TRANSLATE_BURST=1`，必要时降到 `0.5` |
| 想更快但不想冒险 | 用默认的 alimt（QPS 50），把 `SUMMARY_CONCURRENCY` 调大——summary 走千问 MaaS，与翻译额度无关 |

### 并发下的行为保证

- **数据正确性**：每条 Item 由它自己的 goroutine 独占写入，父 goroutine 等所有条目完成后才
  返回，因此调用方随后读切片不会与写入竞争（`go test -race` 全程通过）。
- **产出顺序**：处理顺序与入参顺序无关；下游（向量化、落库）都按下标取用，不依赖顺序。
- **Ctrl-C**：取消后不再对剩余条目发起调用，在飞的请求随 ctx 一起取消并立即返回，
  已处理完的部分照常落库。
- **单条失败**：仍然只降级该条（summary 回退摘要原文、翻译失败留空中文字段），不影响整批。
- **限流**：翻译按 `TRANSLATE_QPS` 均匀放行，突发不超过 `TRANSLATE_BURST`；
  命中限流（HTTP 429 或临时错误码）时仍按原重试策略退避重试（见下一节）。

## 两个外部接口（`translator/` 与 `llm/`）

### 翻译：两种实现可选（`TRANSLATE_PROVIDER`）

`translator.New(translator.Config{...})` 按 `Provider` 返回两种实现之一，两者都实现
同一个 `translator.Client` 接口（`Translate` / `TranslateItem`），因此调用方
（`ingest`、`regen`）完全不需要知道底层用的哪一种：

| provider | 接口 | 认证 | 额度 | 默认限速 / 并发 |
| --- | --- | --- | --- | --- |
| `alimt`（默认） | `alimt 2018-10-12 TranslateGeneral` | RAM AccessKey 的 HMAC-SHA1 签名 | QPS 50 | 50 / 50 / 8 |
| `qwenmt` | 百炼 OpenAI 兼容 chat | `Bearer` + DashScope ApiKey | 60 RPM（≈1 RPS） | 1 / 1 / 2 |

两者的**协议细节不同**，但合并标题与摘要、拆回、失败降级、术语表这四件事是共用实现
（`common.go`），所以切换 provider 不会改变数据流与产出形态。

#### alimt

- **认证**：RAM 的 AccessKey ID/Secret 做 **HMAC-SHA1 签名**（`Signature` 参数），
  不是 `Authorization: Bearer`；请求体是 `application/x-www-form-urlencoded` 表单；
  `SignatureNonce` 每次随机、`Timestamp` 用 UTC。
- **语言码**：`auto`（自动识别）/ `zh` / `en`；`normalizeLang` 同时兼容历史写法
  （`Chinese`、`English`）并做大小写归一。
- **响应**：成功是 `Code=200` + `Data.Translated`；失败是 **HTTP 200 + 非 200 的 `Code`**，
  因此错误判定不能只看状态码（见上一节的错误码表）。

#### qwenmt

- 请求体是 `{"model":"qwen-mt-lite","messages":[...],"translation_options":{...}}`，
  语言码用**全称**（`auto` / `Chinese` / `English`），与 alimt 的短码不同；
- 模型名可用 `QWEN_MT_MODEL` 指定（默认 `qwen-mt-lite`，也支持 `qwen-mt-plus` / `qwen-mt-turbo`）；
- **没有 5000 字符上限**，所以不做分段：整段一次发出（这也是它比 alimt 更适合长摘要的原因之一）；
- **限速是它的主要约束**：只有 60 RPM，且对"请求频率短时间激增"有额外保护。
  只压并发数不够——并发 8、每次 0.3s 仍会在 300ms 内连发 8 个——所以默认就是
  "每秒 1 个请求 + 桶容量 1（不突发）+ 并发 2"，也就是换用 alimt 之前那套控制口径。

**长文本分段（只对 alimt）**：接口单次上限是 **5000 字符**，超了会返回 `10008`。
正式摘要绝大多数在几百字符，但个别长摘要不能整条失败，因此 `splitSegments` 会先把文本
切成不超限的片段、逐段翻译再按顺序拼接。两条硬约束：

1. **分隔符绝不切断**——`pairSeparator` 必须完整落在某一段里，否则标题与摘要就再也
   拆不回来了。所以切分时先用分隔符切一遍，超长的部分再按"段落 → 句子 → 分句 → 词"
   逐级找边界，最后才按字符数硬切；
2. **拼接回来必须逐字等于原文**——请求文本被改动就等于翻译了别的内容。
   边界正则只切在真正的边界上，空白归右段、句末标点归左段。

**术语表**：两种 provider 都走客户端占位符方案（alimt 没有原生 glossary 参数；
qwenmt 虽然支持，但不下发原生参数，避免两套机制互相干扰）——翻译前把术语换成占位符
`zqmarkerq<n>qz`，译文返回后再换回固定译法（`maskGlossary`）。

> 占位符形态是踩坑改出来的：最早用 `XGLOSSARYTERMX0X`，alimt 会把其中的
> `GLOSSARY` 直接译成"术语表"（实测返回 `X术语表X0X`），还原时匹配不上，
> 占位符就漏进了中文字段。现在的形态是纯小写字母 + 数字，既没有可拆的标点，
> 也不像词典里的词，实测两个 provider 都能原样保留。还原仍保留"忽略大小写"的容错。

长术语优先替换，避免 `RAG` 把 `RAGAS` 的前缀吃掉。没有术语时原样返回，不产生额外开销。

### 文献介绍：千问 qwen3.8-flash

- **端点**：`https://maas.qianwenaiapi.com/compatible-mode/v1/chat/completions`
  （千问 MaaS 的 OpenAI 兼容模式）；模型名取 `SUMMARY_MODEL_NAME`（`qwen3.8-flash`）。
- **配置键**：优先读 `SUMMARY_MODEL_NAME` / `SUMMARY_MODEL_API_KEY`，缺失时回退到
  `MODEL_NAME` / `MODEL_API_KEY`。分开是有必要的——后端自己用 `MODEL_NAME` 指它的
  对话模型（DeepSeek），两者含义不同，别让 searcher 误用后端的模型。
- **约束输出改用提示词**，不再用 tool calling：原先靠 `tool_choice` 强制调用
  `generate_user_profile_summary`，现在把"只输出一段连续文本、不要任何前后缀、
  不要 Markdown / JSON / 代码块"写死在系统提示词里，直接读 `message.content`。
  这样少一次函数调用的往返与参数 JSON 转义开销，在 TPM 紧张时更有意义。
- **提示词之外还有兜底**（`extractSummary`）：剥掉代码块围栏与"文献介绍："之类的前缀；
  内容为空、或被 `max_tokens` 截断（`finish_reason=length`）都算失败并重试，
  不让半截话进向量库。若内容落在 `reasoning_content` 里（思考模式没关干净）会显式报错。
- **TPM 控制**：`qwen3.8-flash` 的额度是 **TPM 5M**（按分钟计的 token 数）。
  因此显式 `thinking: disabled`（不开思维链，最大思维链是 262K token，开着会把额度
  全花在推理上）并把 `max_tokens` 限制在 `DefaultSummaryMaxTokens`（1024）。
  `SUMMARY_CONCURRENCY` 是这里唯一的并发阀门，TPM 吃紧时优先调小它。

## 外部接口重试与降级

摘要生成与翻译都会在失败时自动重试（实现在 `retry/`，默认策略 `retry.DefaultConfig()`）：

- **重试次数**：首次失败后再重试 3 次，即最多 4 次调用；退避 2s / 4s / 6s。
  每次重试都会打日志（`生成文献介绍 第 1/4 次尝试失败，2s 后重试: ...`）。
- **摘要**：重试后仍失败（含网络错误、非 2xx、内容为空、被输出上限截断）才放弃，
  由 `ingest` 回退为摘要原文。
- **翻译**：标题与摘要默认合并成一次请求（省一半调用）；若接口没有原样保留分隔符
  导致合并结果无法拆分，会**降级为标题、摘要分别翻译**，多花一次调用换回两段译文。
  降级后只有一边失败时，保留成功的一边，另一边留空并打日志；两边都失败才报错。

要改策略时改 `retry.DefaultConfig()`（或给客户端传 `WithRetryConfig`）：例如把
`MaxRetries` 设为 2 就是"总共最多 3 次调用"。

**翻译的 4xx 不重试**（429 限流除外）：签名无效、AccessKey 没权限、语种不支持这类问题
是配置错误，重试只会白等白花调用量（`retry.Permanent` 负责标记）。机器翻译通用版还会用
**HTTP 200 + 非 200 的 `Code`** 报错，容器同样按错误码区分：

| 处理方式 | 错误码 |
| --- | --- |
| 不重试（配置/参数问题） | `10004` 参数缺失、`10005` 语种不支持、`10008` 字符过长、`10009` 子账号无权限、`10010` 未开通服务、`10013` 未开通或欠费 |
| 重试（临时状况） | `10001` 超时、`10002` 系统错误、`10007` 翻译失败，以及 HTTP 429 |

### 排障提示

- Milvus 没启动时，SDK 默认会一直阻塞建连，最终只报一句 `context canceled`。本脚本给
  连接加了 15s 上限，会直接提示 `连接 Milvus 超时（localhost:19530 超过 15s）: 请确认
  Milvus 已启动、MILVUS_ADDRESS 正确`（可用 `store.Options.ConnectTimeout` 调整）。
- 翻译报 `10009` / `10010` / `10013`：RAM 用户没有 `alimt:TranslateGeneral` 权限，
  或账号还没开通机器翻译服务。前者去 RAM 控制台加权限策略，后者去机器翻译控制台开通。
- 翻译报签名类错误：`ALIYUN_MT_ACCESS_KEY_ID` / `_SECRET` 只配了一半（启动时会直接
  报错提示），或 Secret 复制时多了空格。

## 配置

默认读取 `../backend/.env`（可用 `-config` 指定）。用到的键：

| 键 | 用途 |
| --- | --- |
| `MYSQL_DSN` | MySQL 连接串 |
| `MILVUS_ADDRESS` / `MILVUS_COLLECTION_NAME` / `VEC_DIM` | Milvus 连接、集合与向量维度 |
| `EMBEDDING_MODEL_API_KEY` / `EMBEDDING_MODEL_BASE_URL_WITH_OPENAI` | 向量化 |
| `EMBEDDING_MODEL_NAME` | 向量化模型名（与后端同一键，缺失时回退 `embedder.DefaultModel`） |
| `SUMMARY_MODEL_NAME` / `SUMMARY_MODEL_API_KEY` | 生成文献介绍（千问 MaaS 的 `qwen3.8-flash`，OpenAI 兼容模式）；缺失时回退 `MODEL_NAME` / `MODEL_API_KEY` |
| `MODEL_NAME` / `MODEL_API_KEY` | 后端自己的对话模型（DeepSeek）；searcher 只把它当 summary 的兜底配置 |
| `TRANSLATE_PROVIDER` | 翻译实现：`alimt`（默认，机器翻译通用版）或 `qwenmt`（百炼 Qwen-MT）；留空 = `alimt` |
| `ALIYUN_MT_ACCESS_KEY_ID` / `ALIYUN_MT_ACCESS_KEY_SECRET` | alimt 的 RAM AccessKey（**与上面的百炼/千问 ApiKey 不是同一套凭据**） |
| `QWEN_MT_API_KEY` / `QWEN_MT_MODEL` | qwenmt 的凭据与模型名（默认 `qwen-mt-lite`）；选 `qwenmt` 时必须配 key |
| `ENRICH_CONCURRENCY` | 富化时同时处理的论文数（默认 8；负数 = 不限流） |
| `SUMMARY_CONCURRENCY` | summary 同时在飞的请求数上限（默认 8；负数 = 不限流） |
| `TRANSLATE_CONCURRENCY` | 翻译并发上限；**留空 = 用 provider 默认值**（alimt 8 / qwenmt 2），负数 = 不限流 |
| `TRANSLATE_QPS` / `TRANSLATE_BURST` | 翻译放行速率与桶容量；**留空 = 用 provider 默认值**（alimt 50/50、qwenmt 1/1），`TRANSLATE_QPS` 为负 = 关闭限速 |

缺少摘要凭据时不会中断：只跳过对应步骤并打印警告（summary 回退为摘要原文）。

翻译方面：**完全没配翻译凭据**才跳过翻译（中文字段留空）。若明确选了 provider 却没配
它的凭据（例如 `TRANSLATE_PROVIDER=qwenmt` 但没有 `QWEN_MT_API_KEY`），属于配置错误，
会明确报出来而不是静默跳过——否则会让人以为翻译在跑。alimt 的两个 AccessKey
**只配一半**（只填 ID 或只填 Secret）也属于漏填，启动时直接报错。

缺少存储或 embedding 配置则直接报错退出。

## 用法

```bash
go run . -date 2024-06-01                 # 抓取当天全部论文并入库（自动分页，富化并发 8）
go run . -date 2024-06-01 -max 200        # 最多抓 200 篇
go run . -date 2024-06-01 -concurrency 16 # 富化阶段同时处理 16 篇（覆盖 .env）
go run . -date 2024-06-01 -dry-run        # 只抓取并打印标识与基本信息，不调用 LLM、不写库
go run . -date 2024-06-01 -config ../backend/.env

go run . -from 2024-06-20 -to 2025-01-20  # 按日期范围收集（含首尾，逐天入库）
go run . -from 2024-06-20 -to 2024-06-30 -dry-run   # 先试跑看看每天抓多少篇
```

`-date` 指 arXiv `submittedDate` 的当天，按美东时间（ET）计。

### 按日期范围收集（`-from` / `-to`）

`-from` 与 `-to` 必须成对出现，范围**含首尾**，按天升序逐天执行"抓取 → 去重 → 富化 → 入库"，
每天是一批独立的入库（不是把整个范围合成一次抓取）：

- **同一天只跑一次**：`-from 2024-06-20 -to 2024-06-20` 等价于 `-date 2024-06-20`；
- **与 `-date` 互斥**：同时给出会直接报错，避免"到底跑哪天"的歧义；
- **`-max` 仍然按天生效**：`-max 50` 表示范围里每天最多 50 篇，0 表示每天全量；
- **某天失败不中断整轮**：已成功的日期照常入库，失败的日期在末尾汇总列出并让程序以非零码退出，
  重跑同样的 `-from/-to` 即可补齐（已入库的论文会被去重跳过，不会重复写入）；
- **Ctrl-C**：停止调度剩余日期，在飞的请求随 ctx 取消，结束时打印"已完成/剩余几天"。

一次 2024-06-20 ~ 2025-01-20 的范围约 215 天、每天几百到上千篇。按富化默认并发 8 估算，
单日几百篇需要几分钟，整轮是数小时到十几小时的量级（取决于当天论文数）——建议用
`tmux`/`nohup` 之类挂着跑，中途断了重新执行同一命令即可续上。

为什么逐天串行而不是并行多天：每天本来就是独立的一批，并行多天只会让 arXiv 与 LLM 的瞬时
压力叠加、更容易被限流；真正耗时的富化在**单天内部**已经是并发的（见"并发加速"一节）。

### 维护操作（`-op`）

不抓取论文，直接操作已有数据，共用同一份 `.env`：

```bash
go run . -op stats                              # 查看总共有多少条 item 记录（MySQL 行数 + Milvus 向量数）
go run . -op dates                              # 查看记录覆盖了哪些日期（按日期倒序，含软删除条数）
go run . -op delete-date -date 2024-06-20       # 报告这一天的规模（不删除）
go run . -op delete-date -date 2024-06-20 -yes  # 删除这一天的全部记录
go run . -op purge -yes                         # 清空全部 item 记录（MySQL 硬删除 + Milvus 集合重建）

# 换模型后刷新已有数据（详见下一节；不加 -yes 只报告规模）
go run . -op regen-translate -date 2024-06-20 -yes
go run . -op regen-summary   -date 2024-06-20 -yes
go run . -op regen-embedding -date 2024-06-20 -yes
```

`-op purge` / `-op delete-date` 不带 `-yes` 时只报告将要删除的规模并退出（不删任何东西），避免误删。

### 按日期重新生成（`-op regen-*`）

**为什么需要它**：`ingest` 只处理"库里还没有的"论文（先按标识去重再富化），所以换了
模型之后**已入库的记录永远不会被刷新**，检索会继续用旧模型的产物。这三个操作就是补这一块：
按 `publish_date` 把已有记录捞出来，重算指定字段再回写。

```bash
go run . -op regen-translate -date 2024-06-20        # 报告这一天有多少条（不调用接口）
go run . -op regen-translate -date 2024-06-20 -yes   # 重新生成中文翻译
go run . -op regen-summary   -date 2024-06-20 -yes   # 重新生成文献介绍
go run . -op regen-embedding -date 2024-06-20 -yes   # 重新生成向量（Milvus）
```

| 操作 | 重算什么 | 输入 | 写到哪里 |
| --- | --- | --- | --- |
| `regen-translate` | `title_chinese` / `abstract_chinese` / `is_chinese` | 原始 `title` + `abstract` | MySQL |
| `regen-summary` | `summary` | 原始 `title` + `abstract` | MySQL |
| `regen-embedding` | 向量 | 库里**当前的 `summary`** | Milvus（按主键先删后插） |

**顺序很重要**：向量是对 `summary` 编码出来的，所以换了摘要模型后必须
**先 `regen-summary`、再 `regen-embedding`**，否则向量编码的还是旧 summary。
`regen-embedding` 会读当前值而不是重算 summary，正是为了让这两步可以分开跑、分开重试。

其他行为：

- **默认只报告规模**：不带 `-yes` 时打印"这一天有多少条将重新生成什么"然后退出，
  不调用任何接口、不写库（`-op regen-*` 会覆盖已有字段并消耗接口额度，误敲日期代价不小）；
- **按日期逐条回写，不是整行覆盖**：只更新该操作负责的列。重新生成期间别的进程改过的
  其他字段（比如 `keywords`）不会被写回旧值；
- **单条失败只降级该条**：翻译失败的那条跳过不写（不拿空串覆盖已有译文），其余照常；
  summary 失败则回退为"摘要原文，其次标题"并**照常写入**（它有一个可用的值，
  丢掉反而会让这条继续停在旧模型的产物上），失败条目会逐条打日志便于重跑；
- **`-concurrency` 同样生效**：控制同时处理的记录数（默认 `ENRICH_CONCURRENCY`），
  各接口自己的并发闸门与限速仍然独立生效；
- **可重复执行**：中途 Ctrl-C 或失败后重跑同一条命令即可，已处理的记录重算一遍也不会出错；
- **向量替换是先删后插**：同一个 id 不会留下新旧两条向量（并存会让检索结果不稳定）；
  中途失败的最坏结果是"该条暂时没有向量"（检索不到，但可重跑补齐）。

真实数据上的耗时参考（2 条记录，含网络往返）：翻译约 1s、summary 约 2s、embedding 约 1s。


### 按日期删除（`-op delete-date`）

删除指定 `publish_date` 的**全部**记录：MySQL 硬删除 + Milvus 按主键删除对应向量。
典型用法是"某天抓坏了就删掉重抓"，例如接口限流导致某天大量 summary 回退成摘要原文：

```bash
go run . -op delete-date -date 2024-06-20 -yes   # 删掉这一天
go run . -from 2024-06-20 -to 2024-06-20         # 重新收集这一天
```

几个实现上的取舍：

- **只删这一天的主键，不重建集合**：与 `purge` 不同，集合、索引、其他日期的向量都不动，
  因此删除期间不影响检索服务；
- **是硬删除，不是软删除**：软删除的行在去重时仍算"已存在"，软删之后这一天的论文再也抓不回来；
  硬删除才能配合"删掉 → 重新收集"的用法；
- **日期必须是真实存在的 `YYYY-MM-DD`**：`publish_date` 是字符串列，拼错的日期只会"删除 0 条"
  而看起来像成功，所以入口处直接拒绝 `2024-02-31`、`2024/06/20` 这类输入；
- **执行顺序是先 MySQL 后 Milvus**：中途失败留下的是孤儿向量（检索命中后回表查不到记录会被跳过，
  无害），而不是"有行但检索不到"的记录；此时 `-op stats` 会显示 Milvus 比 MySQL 多，
  重跑无用（该日期已无 MySQL 记录），需要用 `-op purge` 重建集合清干净。

几个实现上的取舍：

- **purge 对 MySQL 是硬删除**：软删除的行在去重时仍算"已存在"，只软删会导致清空后
  同一篇论文再也抓不回来。
- **purge 直接删掉 Milvus 集合并按同一 schema 重建**（含索引与 load），历史遗留的
  孤儿向量也一并清掉，清空后仍是"空但可用"的状态。
- **自增 ID 不重置**：重置会让新 item 复用旧 ID，而后端 Redis 里可能还留着按 item id
  记录的旧数据。
- **stats / purge 计数前会 Flush 一次**：Milvus 中尚未落盘（增长段）的向量不参与
  `count(*)`，刚写完就读会得到 0。
- `stats` / `dates` 不会创建 Milvus 集合（`store.Options.SkipCollectionInit`），
  只读命令不产生副作用。

## 测试

```bash
go test ./...              # 单元测试（全部离线，用 mock 服务代替外部接口）
go test -race ./...        # 同上，带竞态检测（并发富化必须过这一关）
gofmt -l . && go vet ./...
```

`maintain` 的操作逻辑用假 Repository 做了单元测试；`store/store_integration_test.go`
是可选的真实依赖用例，默认跳过；它会重建指定的
Milvus 集合，务必指向一次性资源：

```bash
mysql -e "CREATE DATABASE litstream_searcher_it CHARACTER SET utf8mb4;"
SEARCHER_IT_DSN='user:pass@tcp(127.0.0.1:3306)/litstream_searcher_it?charset=utf8mb4&parseTime=True&loc=Local' \
SEARCHER_IT_MILVUS_ADDRESS=127.0.0.1:19530 \
SEARCHER_IT_COLLECTION=litstream_searcher_it \
SEARCHER_IT_VEC_DIM=4 \
go test ./store/ -run Integration -v
```

## 与后端保持一致

改动以下内容时必须同步后端，否则检索会失效或读不出数据：

- `domain.Item` 的列名 / 类型 / gorm tag ↔ 后端 `domain.Item`（`items` 表）；
- Milvus 集合 schema 与索引（`id` int64 主键 + `vec` 浮点向量，HNSW/L2，M=16，efConstruction=64）
  ↔ 后端 `InitMilvus`；
- 向量化模型（`EMBEDDING_MODEL_NAME`）↔ 后端 `internal/embedutil`，必须同一模型，否则向量不在同一语义空间。

## 两个接口的实测坑（改代码前先看这里）

下面两条都是**真实调用接口后才发现**的行为，文档里没写，踩过一次：

**1. `Code` 字段的类型是字符串，不是文档写的整数。**
`TranslateGeneral` 实测返回 `{"Code":"200", ...}`，而文档把它写成 integer。
结构体若声明成 `int` 会以
`cannot unmarshal string into Go struct field ... of type int` 解析失败，
于是**每一次成功的翻译都被当成失败并重试 4 次**。现在用 `codeValue` 兼容数字与字符串。

**2. 机器翻译会重排分隔符与占位符。**
`|||SPLIT_MT|||` 回来时可能是 ` ||| SPLIT_MT ||| `，也可能把开头的竖线并成一个
（` | SPLIT_MT | | `）；术语占位符还会被改大小写（`XGLOSSARYTERMX0X` → `Xglossarydermx0x`）。
所以「拆回标题/摘要」用 `findSeparatorSpan`（先认骨架 `SPLIT_MT`、再向两侧吃掉竖线与空白），
「还原术语」用忽略大小写的正则替换。按精确字符串匹配会分别造成
"白白降级重译"和"占位符泄漏进中文字段"。

**3. embedding 批量返回的 `index` 全是 0。**
百炼兼容接口（实测 `qwen3.7-text-embedding-flash`）对批量输入的每一条都返回
`index: 0`。按 `index` 归位会把下标 0 反复覆盖、其余位置留空（向量静默丢失）。
现在只在 index 构成合法排列时才按 index 排，否则按返回顺序一一对应。
顺带记录：该接口批量上限是 **25**（26 会 400），`embedder.BatchSize=10` 是安全的。
