# 技术栈

[English](tech-stack.md) · [中文](tech-stack.zh-CN.md)

YuStream 有意采用一套小型、常规的技术栈。没有模型训练框架、没有特征存储、不依赖 Kubernetes，应用层也不需要 GPU。

## 概览

| 层次 | 选择 | 版本 / 镜像 |
| --- | --- | --- |
| 前端 | Vue 3 + Vite + Pinia + Vue Router + Axios | Vue 3.5.13, Vite 6.4.3, Pinia 2.3.1, Vue Router 4.6.4, Axios 1.7.9 |
| 数学渲染 | KaTeX | 0.18.7 |
| 后端 | Go + Gin | Go 1.26.3, Gin 1.12.0 |
| ORM | GORM + MySQL 驱动 | GORM 1.31.1, 驱动 1.6.0 |
| 缓存 / 流 / 队列 | Redis + go-redis + asynq | Redis 7 Alpine, go-redis 9.20.0, asynq 0.26.0 |
| 向量数据库 | Milvus standalone + Go SDK | Milvus 2.5.8, SDK v2.4.2 |
| 关系型数据库 | MySQL | 8.0 |
| Milvus 依赖 | etcd + MinIO | etcd 3.5.18, MinIO `RELEASE.2023-03-20T20-16-18Z` |
| 日志 | zerolog + lumberjack | zerolog 1.35.1 |
| 配置 | Viper | 1.21.0 |
| 认证 | JWT + bcrypt | golang-jwt/jwt v4.5.2, x/crypto |
| 校验 | go-playground/validator | 10.30.1 |
| 参考离线入库流程（可选） | 独立的 Go module（`searcher/`） | 自己的 `go.mod`，相同的 Go 版本 |
| 本地编排 | Docker Compose | Compose v2 |

## 前端

| 关注点 | 实现 | 位置 |
| --- | --- | --- |
| 框架 | Vue 3 Composition API，`<script setup>` | `frontend/src/` |
| 构建/开发服务器 | Vite 6 | `frontend/vite.config.js` |
| 状态 | Pinia store（`auth`、`feed`、`preferences`、`ui`） | `frontend/src/stores/` |
| 路由 | Vue Router 4，history 模式 | `frontend/src/router/index.js` |
| HTTP | 带 JWT 拦截器和 401 处理器的 Axios 实例 | `frontend/src/api/client.js` |
| 样式 | 手写 CSS 与设计 token；无 UI 框架 | `frontend/src/styles/base.css` |
| 动画 | CSS transition + `IntersectionObserver` reveal 指令 | `frontend/src/directives/reveal.js` |
| 平滑滚轮 | 自定义 `requestAnimationFrame` 缓动 | `frontend/src/composables/useSmoothWheel.js` |
| 数学 | KaTeX 加一个小的后备渲染器 | `frontend/src/components/MathText.vue`、`utils/mathText.js` |

SPA 是静态构建产物（`dist/`）。它期望 `/api` 同源：Vite 在开发时对其进行代理，生产环境应由反向代理做同样的事。后端没有 CORS 中间件。

## 后端

| 关注点 | 实现 | 位置 |
| --- | --- | --- |
| HTTP | Gin，分组的 `/api` 路由，JWT 中间件 | `backend/api/` |
| 业务编排 | `service` 层，每个方法一个用例 | `backend/internal/service/` |
| 数据访问 | `repository` 层（MySQL、Redis、Milvus） | `backend/internal/repository/` |
| 异步任务 | 基于 Redis 的 asynq 生产者/消费者 | `backend/internal/queue/`、`backend/internal/worker/` |
| LLM 提示词与调用 | `llm` 包，强制工具调用 | `backend/llm/` |
| 提供方客户端 | 最小的 OpenAI 风格聊天客户端 | `backend/pkg/llm.go` |
| Embedding | 指向 DashScope 兼容端点的 `go-openai` 客户端 | `backend/internal/util/embedutil/` |
| 邮件 | 阿里云 DirectMail SDK | `backend/internal/util/mailutil/` |
| 限流 | Redis Lua 固定窗口限流器 | `backend/api/middleware/rate_limit_middleware.go` |
| 日志 | 带请求 ID 的 zerolog；GORM/asynq 适配器 | `backend/pkg/mylog/` |
| 启动装配 | 单一组合根 | `backend/bootstrap/app.go` |

HTTP 服务器和 asynq worker 运行在同一进程中。`bootstrap.StartServer` 在 goroutine 中启动 worker，然后执行 `gin.Run`。asynq 配置为并发 50，每个任务最多重试 3 次。

## 数据层

### MySQL 8.0

- `users` 和 `items` 在启动时通过 GORM `AutoMigrate` 创建/更新。
- 数据库本身**不**由 AutoMigrate 创建。Docker Compose 通过 `MYSQL_DATABASE` 创建 `litstream`；手动部署必须先创建它。
- `users.email` 唯一。`items.url` 实际上是唯一的目录标识符（LitStream 使用 arXiv ID），尽管它不是数据库唯一索引。
- 连接池设置可配置：默认空闲连接 20、打开连接 50、生命周期 30 分钟、空闲时间 10 分钟。

### Redis 7

Redis 承载四类不同的状态：

1. **缓存**：画像 hash、物品 JSON、话题向量。
2. **交互日志**：每个用户一个 Stream，外加一个指针键。
3. **推荐状态**：当前精排窗口、窗口计数器、最近推荐 ZSET。
4. **任务队列**：asynq 的列表/队列。

Compose 中启用了 AOF 持久化（`--appendonly yes`），因此排队任务在重启后仍然存在。

### Milvus 2.5.8

- Standalone 模式，使用 etcd 存储元数据，使用 MinIO 作为对象存储。
- 一个集合（`MILVUS_COLLECTION_NAME`），schema 为 `id: Int64` + `vec: FloatVector(VEC_DIM)`。
- HNSW 索引，L2 度量，`M=16`，`efConstruction=64`。
- 启动时加载集合。searcher 在写入时确保使用相同的 schema 和索引。

## AI 服务

| 用途 | 默认 | 可配置？ |
| --- | --- | --- |
| 画像生成/更新、精排 | DeepSeek chat completions（`https://api.deepseek.com/chat/completions`） | 模型和 API key 通过 `.env` 配置；**端点在 `backend/pkg/llm.go` 中硬编码** |
| 物品摘要生成（离线） | 通过 Qwen MaaS OpenAI 兼容端点使用 Qwen `qwen3.8-flash` | 模型/key 通过 `SUMMARY_MODEL_*` 配置；**端点在 `searcher/llm/client.go` 中硬编码** |
| Embedding | 通过 DashScope OpenAI 兼容端点使用 `qwen3.7-text-embedding-flash` | 名称、key 和 base URL 通过 `EMBEDDING_MODEL_*` 配置；维度通过 `VEC_DIM` 配置 |
| 中文翻译 | Qwen-MT（`qwen-mt-lite`）或阿里云机器翻译 | `TRANSLATE_PROVIDER` 加上提供方凭据 |

没有本地推理。每次 LLM、embedding 和翻译调用都是网络请求。应用层可以运行在仅有 CPU 的主机上；内存开销主要由 Milvus、MySQL 和 Redis 决定。

## 参考离线流水线（`searcher/`，可选）

searcher 是一个独立的 Go module，有自己的 Dockerfile，并且不依赖导入后端。它共享同一个 `.env`，写入相同的 MySQL/Milvus。它是一个具体的生产者，不是运行时的必需部分：任何写入物品契约（`raw → semantic text → vector`，MySQL/Milvus 中 ID 相同）的流水线都是兼容的。

| 包 | 职责 |
| --- | --- |
| `arxiv/` | arXiv Atom API 客户端：分页、重试、IP 回退、XML 解析 |
| `llm/` | 摘要生成客户端 |
| `translator/` | `alimt` 和 `qwenmt` 翻译提供方 |
| `embedder/` | 批量 embedding（批大小 10） |
| `ingest/` | 获取 → 映射 → 去重 → 丰富 → embedding → 存储 |
| `store/` | MySQL + Milvus 写入、去重查询、维护 |
| `regen/` | 为某个日期重新生成翻译/摘要/向量 |
| `maintain/` | `stats`、`dates`、`delete-date`、`purge` |
| `semaphore/` | 按提供方的并发门和限流器 |
| `retry/` | 带不可重试错误标记的退避重试 |

## 部署与网络

Docker Compose 服务和主机端口：

| 服务 | 容器 | 主机端口 | 说明 |
| --- | --- | --- | --- |
| MySQL | `litstream-mysql` | 内部 3306 | 默认不向主机发布 |
| Redis | `litstream-redis` | 内部 6379 | 默认不向主机发布 |
| etcd | `milvus-etcd` | 内部 2379 | Milvus 元数据 |
| MinIO | `milvus-minio` | `9000`、`9001` | 对象存储与控制台 |
| Milvus | `milvus-standalone` | `19530`、`9091` | gRPC 与健康/指标 |
| 后端 | `litstream-backend` | `127.0.0.1:8080` | 仅绑定回环地址 |
| searcher | `litstream-searcher` | 无 | `tools` profile，一次性任务 |

前端**不**包含在 Compose 文件中。Nginx 服务已被注释掉，其引用的 Dockerfile/配置也不存在。请用你自己的 Nginx/Caddy 提供 `frontend/dist`，或在本地使用 `yarn dev`。后端绑定到回环地址，因此公网访问必须在其前面放置反向代理（或像演示部署中那样使用 frp 隧道）。

## 版本兼容性说明

- `go.mod` 声明 Go 1.26.3；Docker 构建器使用 `golang:1.26`。
- Milvus Go SDK 是 v2.4.2，而服务器是 v2.5.8。这里使用的子集（集合创建/加载、HNSW 索引、插入/搜索/查询）可以工作，但不要假设 SDK 提供仅 2.5 才有的新 API。
- `VEC_DIM` 必须与 embedding 模型输出以及现有 Milvus 集合匹配。修改它需要新建集合并完整重新 embedding。
- 前端需要 Node.js 18+。仓库包含 `yarn.lock`；使用 Yarn 1.x 可获得最接近的复现，或者如果你接受不同的 lockfile，也可以使用 npm。

## 有意缺失的内容

- TypeScript、SSR 和组件库。
- 独立的模型服务或特征服务。
- 通用入库连接器。`searcher/` 是 arXiv 参考实现；你的加载器可以是任何东西。
- Kafka/RabbitMQ（在这个规模下 Redis + asynq 已经足够）。
- CORS（假定使用同源代理）。
- 提示词注册表或热重载机制。
- 内置评估工具或基准测试套件。

这些省略让部署保持精简；如果项目超出原型阶段，它们也是首先要补充的内容。
