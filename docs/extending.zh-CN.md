# 扩展与优化 YuStream

[English](extending.md) · [中文](extending.zh-CN.md)

YuStream 是一个起点，而不是固定的处方。有两种正当的构建方式：

1. **案例扩展**——为新的垂类或新的产品场景实例化这套框架。
2. **框架优化**——改进核心闭环本身：检索、精排、画像、服务、成本、评估或运维。

两者同等重要。新增垂类不需要 fork 项目；改进框架也不需要保留当前的每一个组件。

## 1. 两条路线

| | 案例扩展 | 框架优化 |
| --- | --- | --- |
| 目标 | 交付一个新的垂类推荐流 | 让框架更快、更省、更好或更通用 |
| 典型负责人 | 产品 / 应用开发者 | 框架维护者 / 研究者 |
| 起点 | [垂类复用指南](reuse-guide.zh-CN.md) | 本文档 + [架构](architecture.zh-CN.md) |
| 改动内容 | 数据源、物品映射、提示词、UI 文案、交互集合 | 检索、精排器、画像引擎、队列、缓存、存储、可观测性 |
| 应保持稳定的 | 物品契约、API 契约、工具调用契约 | 同样的契约，除非你有意对迁移做版本化 |
| 需要 fork 吗？ | 通常不需要 | 深层改动通常需要；较小的改动可以回流上游 |

## 2. 案例扩展：新增一个垂类

案例扩展使用现有运行时，只改动领域层：

- **目录与入库**——从你自己的来源导入物品。`searcher/` 只是一个参考生产器，并非必需。
- **物品 schema**——把你的字段映射到现有 `items` 结构，或在后端/searcher/前端新增字段。
- **语义文本**——选定那唯一一段既用于嵌入、又展示给精排器的文本。
- **提示词**——重写画像生成、画像更新、字段变更更新和精排提示词。
- **交互模型**——决定哪些行为重要，以及如何记录它们。
- **UI**——卡片、详情面板、筛选、文案和交互入口。
- **评估**——为你的领域定义相关性；不要沿用论文场景的判断标准。

详细步骤见[垂类复用指南](reuse-guide.zh-CN.md)。

### 唯一必须遵守的不变式

无论你选择哪条入库路径，运行时都期望这条链路：

```text
raw item information
      │  (your source: API, crawl, CMS, CSV, queue, manual, searcher, ...)
      ▼
item semantic text
      │  (summary / enriched description / title + attributes)
      ▼
embedding vector
      │
      ├── MySQL: items row (same id, display fields, semantic text)
      └── Milvus: (id, vec) with the same id
```

后端不 import searcher，它只读 MySQL 和 Milvus。如果你的管线产出这个契约，它就能与 YuStream 兼容。可以借鉴流程，不必照搬 searcher 代码。

## 3. 框架优化：改进核心

闭环中有多个可替换的部件。只要保持第 4 节的契约，你就可以替换其中任何一个部件背后的实现。

| 优化目标 | 入口 | 改动示例 |
| --- | --- | --- |
| 入库 | `searcher/` 或你自己的写入器 | 增加连接器、流式入库、增量更新、按 SKU 去重 |
| 嵌入 | `embedutil`、`searcher/embedder` | 新模型、维度、批处理策略、本地嵌入服务 |
| 召回 | `ItemRepository.SearchVec`、`MilvusDB` | 混合 BM25 + 向量、元数据过滤、多 collection、ANN 调参 |
| 精排 | `LLMInvoking.Recommend`、`BuildToolRecommend` | 新提示词、listwise 与 pairwise、多样性/MMR、确定性兜底 |
| 画像 | `PromptGenerateUS`、`PromptUpdateUS`、`UserService` | 结构化画像、衰减、显式用户控制、多画像 |
| 反馈 | `InteractionRepository`、`PromptUpdateUS` | 事件加权、防抖、会话化、负反馈 |
| 交付 | `RecommendCache`、`ItemService.GetRecommend` | 按窗口的 key、预取、预热、流式传输 |
| 队列 / worker | `queue/`、`worker/`、`bootstrap` | 独立 worker 进程、优先队列、按提供方区分的并发控制 |
| 存储 | `db/`、repositories | 只读副本、Redis 淘汰策略、每个垂类一个 collection |
| 成本 | 提示词、`SEARCH_TOP_K`、TTL | 候选裁剪、更短的摘要、自适应的更新节奏 |
| 可观测性 | `pkg/mylog`、中间件 | 指标、链路追踪、token 计费、分阶段延迟 |
| 安全 | 提示词/工具层、API | 内容审核、允许列表、PII 脱敏、审计日志 |

### 常见优化方向

**成本。** 最大的经常性成本是精排提示词（候选数 × 摘要长度），其次是画像更新和离线富化。减少候选、缩短摘要、提高缓存复用，或降低画像更新频率。

**延迟。** 同步路径已经避免了等待 LLM。接下来的收益来自嵌入缓存命中、更快的物品加载、独立的 worker 进程，以及在用户滑到底部之前预热下一个窗口。

**质量。** 增加词汇/向量的混合召回、让精排集合更多样、按置信度给交互加权，并在固定候选集上评估提示词变体。不要在不同召回结果上比较变体。

**规模。** 把 worker 移出 HTTP 进程、按垂类分片 Milvus、增加 MySQL 只读副本，并按 key 族分区 Redis。保持物品 ID 映射不变。

**通用性。** 外置提示词、增加提供方适配器、用 `vertical_id` 和 collection 路由支持多垂类，并让缓存 key 包含垂类。

**运维。** 增加 `/metrics`、结构化的 token 计费、分阶段耗时，并针对空窗口、LLM 错误率、队列深度和 Milvus 负载状态设置告警。

## 4. 必须保留的契约

除非你有意对迁移做版本化，否则请保持以下内容稳定：

### 物品契约

- MySQL `items.id` 是连接键。
- Milvus `(id, vec)` 使用**同一个** `id`。
- 用于嵌入的语义文本存放在物品上（在 LitStream 中是 `items.summary`）。
- 展示用的元数据放在 MySQL；Milvus 不需要它。
- 不要把不同嵌入模型/维度的向量混在一个 collection 中。

### API 契约

- `GET /api/items/recommend` 返回 `{ items, source, outline }`。
- `source` 取值为 `llm`、`coarse` 或 `empty`。
- 除非你对 API 做版本化，鉴权仍基于 JWT。
- 前端不假设有游标；每个请求都是一个新的窗口。

### LLM 契约

- 精排工具调用返回 `items_list`（从 1 开始的索引）和 `outline`。
- 画像工具调用返回 `summary` 和 `interest_topics`。
- 服务端会对索引做边界检查，并把它们映射回给定的候选。
- 画像 topics 是唯一的召回查询；保持简短、格式稳定。

### 状态契约

- `recommend:current` 是每个用户一个缓存槽（除非你改变 key 模型）。
- `recentlyrecommended` 的分数含义是“距上次展示过了几轮”，不是物品数量。
- 交互流指针只在画像写入成功后才前进。
- Redis key 包含用户 ID；若增加多租户，请在 key 中加入租户/垂类。

### 分层契约

- Controller 和 worker 是薄适配层。
- 业务决策放在 `service`。
- Repository 不调用 LLM，也不入队任务。
- 提示词放在 LLM 包中，不放在 controller 里。

完整的分层映射见[后端结构](backend-structure.zh-CN.md)。

## 5. 分支、版本化与迁移

- **何时 fork**：当你改变存储 schema、提供方协议或服务拓扑时。较小的改动（一个提示词、一个连接器、一个指标）可以回流上游。
- **提示词版本化**：如果要做 A/B 测试，用配置开关或提示词注册表。不要在生产环境悄悄改提示词。
- **嵌入迁移**：新建 collection、重新嵌入全部数据、使用户向量失效，并在评估通过后再切换流量。
- **物品 schema 迁移**：用显式回填；AutoMigrate 只新增列，不转换数据。
- **保留回滚路径**：每次迁移都要有（旧 collection、旧提示词版本、旧 worker 镜像）。

## 6. 评估护栏

任何框架优化都应回答：

- 在固定评估集上，召回是否提升？
- 精排窗口的 precision/NDCG 或线上互动是否提升？
- token、成本和延迟发生了什么变化？
- 空窗口率或工具调用格式错误率是否变化？
- 该改动是否在至少两个垂类或用户群上成立？

同时收集离线和线上信号；某个提示词或模型改动在单个例子上看起来更好，整体上可能更差。

## 7. 贡献路径

有价值的贡献包括：

- 一个新的垂类示例（包含提示词、映射和示例数据契约）。
- 一个提供方适配器（OpenAI 兼容的 base URL、鉴权、工具调用差异）。
- 一个更好的精排提示词，附带小型评估集。
- 一项检索改进（混合检索、过滤、ANN 调参）。
- 可观测性、测试和失败模式修复。
- 文档、图表和翻译。

提交代码改动时，请遵守分层规则，并附上你使用的评估。新增垂类时，请附上提示词集和物品映射，以便他人复现。

## 8. 注意事项

- 不要把 `searcher/` 当作必需项。它只是针对 arXiv 的一个参考生产器。
- 不要让 LLM 编造物品 ID 或绕过边界检查。
- 不要把两个嵌入模型写入同一个 collection。
- 不要在未更新文档和 ZSET 迁移的情况下改变 `RECS_WINDOW` 的语义。
- 不要把只对单一垂类有效的功能加进框架核心而没有配置边界。
- 不要在缺少固定评估集和成本检查的情况下宣称改进。

YuStream 本就是用来 fork、扩展和改进的。闭环才是稳定的理念；它周围的组件随你改动。
