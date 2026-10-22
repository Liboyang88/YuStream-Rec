# 配置

[English](configuration.md) · [中文](configuration.zh-CN.md)

所有运行时配置都从 `.env` 文件读取。后端使用 Viper 的 `SetConfigFile(".env")`，因此该文件必须位于进程工作目录（Compose 把它挂载到 `/app/.env`）。searcher 默认读取 `../backend/.env`，并接受 `-config <path>`。

从脱敏模板开始：

```bash
cp .env.example .env
```

永不提交 `.env`。仓库的 `.gitignore` 已在根目录和 `backend/` 中排除了它。

## 1. 本地整套服务跑通所必需

以下取值必须在首次注册与首次 feed 之前就是真实可用的：

| 变量 | 示例 | 说明 |
| --- | --- | --- |
| `ACCESS_TOKEN_SECRET` | 长的随机字符串 | HS256 签名密钥。空值会生成不安全的 token。 |
| `REFRESH_TOKEN_SECRET` | 长的随机字符串 | Refresh token 签名密钥；目前尚未实现 refresh 路由。 |
| `MODEL_API_KEY` | `sk-...` | 后端 LLM 密钥（DeepSeek 端点已硬编码）。 |
| `MODEL_NAME` | 你所选服务商的模型名 | 必须是该密钥可访问的模型。示例使用 `deepseek-flash`。 |
| `EMBEDDING_MODEL_API_KEY` | `sk-...` | 用于 embedding 的 DashScope 密钥。 |
| `EMBEDDING_MODEL_NAME` | `qwen3.7-text-embedding-flash` | 必须与 `VEC_DIM` 和 Milvus collection 匹配。 |
| `EMBEDDING_MODEL_BASE_URL_WITH_OPENAI` | `https://dashscope.aliyuncs.com/compatible-mode/v1` | OpenAI 兼容的 base URL；客户端会追加 `/embeddings`。 |
| `ALIYUN_ACCESS_KEY_ID` | `LTAI...` | 用于注册/重置邮件的 DirectMail AccessKey。 |
| `ALIYUN_ACCESS_KEY_SECRET` | 密钥 | DirectMail 密钥。 |
| `MAIL_ACCOUNT_NAME` | `litstream@your-domain` | 已验证的 DirectMail 发信地址。 |
| `SUMMARY_MODEL_API_KEY` | `sk-...` | searcher 摘要密钥。摘要端点是 Qwen MaaS 端点，不是 DeepSeek。 |

## 2. 后端变量

### 服务器与认证

| 变量 | 默认值 | 含义 |
| --- | --- | --- |
| `SERVER_ADDRESS` | `:8080` | Gin 监听地址。Compose 发布 `127.0.0.1:8080`。 |
| `ACCESS_TOKEN_SECRET` | — | Access JWT 密钥。 |
| `REFRESH_TOKEN_SECRET` | — | Refresh JWT 密钥。 |
| `ACCESS_TOKEN_EXPIRE_HOUR` | `1` | Access token 有效期（小时）。 |
| `REFRESH_TOKEN_EXPIRE_HOUR` | `72` | Refresh token 有效期（小时）。SPA 会保存它，但不会调用 refresh 端点。 |
| `LOG_LEVEL` | `info` | zerolog 级别（`debug`、`info`、`warn`、`error`）。 |

### MySQL

| 变量 | 默认值 | 含义 |
| --- | --- | --- |
| `MYSQL_DSN` | Compose 示例：`new_user:123456@tcp(mysql:3306)/litstream?...` | 必须与 Compose 的数据库/用户/密码一致。 |
| `MYSQL_MAX_IDLE_CONNS` | `20` | `SetMaxIdleConns`。 |
| `MYSQL_MAX_OPEN_CONNS` | `50` | `SetMaxOpenConns`。 |
| `MYSQL_CONN_MAX_LIFETIME` | `30` | 分钟。 |
| `MYSQL_CONN_MAX_IDLE_TIME` | `10` | 分钟。 |

Compose 文件通过 `MYSQL_DATABASE` 创建 `litstream` 数据库。AutoMigrate 会创建/更新表，但**不会**创建数据库。如果你自己运行 MySQL，请先创建数据库。

Compose 的 MySQL 凭据硬编码在 `docker-compose.yml` 中（`root` / `123456`，用户 `new_user` / `123456`）。如果你修改了它们，也要同步更新 `MYSQL_DSN`。

### Redis

| 变量 | 默认值 | 含义 |
| --- | --- | --- |
| `REDIS_ADDRESS` | `redis:6379` | Redis host:port。 |
| `REDIS_PASSWORD` | 空 | Redis AUTH 密码。 |
| `REDIS_DB` | `0` | Redis 逻辑数据库。 |
| `USER_NAME_TTL` | `72` | 小时。尽管名字如此，它实际是 `email:{email}` → 用户 ID 缓存的 TTL。 |
| `USER_INFO_TTL` | `72` | 小时。`user:{id}` 画像哈希与主题向量的 TTL。 |
| `ITEM_TTL` | `168` | 小时。`item:{id}` 元数据缓存的 TTL。 |
| `INTERACTION_TTL` | `336` | 小时（14 天）。交互 stream 与指针的 TTL。 |
| `RECOMMEND_TTL` | `30` | **分钟。** 当前精排窗口的 TTL。 |
| `INTERACTION_RECORD_MAX_LEN` | `40` | 每条交互 stream 保留的最大物品数。 |
| `QUEUE_UNIQUE_TTL1` | `60` | 秒。画像任务的 asynq 唯一性窗口。 |
| `QUEUE_UNIQUE_TTL2` | `60` | 秒。精排任务的 asynq 唯一性窗口。 |

### Milvus 与推荐形态

| 变量 | 默认值 | 含义 |
| --- | --- | --- |
| `MILVUS_ADDRESS` | `milvus:19530` | Milvus gRPC 地址。 |
| `MILVUS_COLLECTION_NAME` | `litstream` | Collection 名称。每个部署一个垂直领域。 |
| `VEC_DIM` | `1024` | 必须与 embedding 模型输出和 collection 一致。 |
| `SEARCH_TOP_K` | `100` | 从 Milvus 召回并交给精排器的候选数量。 |
| `LIMIT_TOP_K` | `18` | 要求精排器选出的物品数，也是每个 LLM 窗口最多返回的数量。 |
| `RECS_WINDOW` | `10` | 防重复窗口，单位是**精排轮次**而非物品数。当 `LIMIT_TOP_K=18` 时，最多可排除约 180 个物品。 |

### LLM 与 embedding

| 变量 | 默认值 | 含义 |
| --- | --- | --- |
| `MODEL_NAME` | 示例中为 `deepseek-flash` | 后端对话模型。 |
| `MODEL_API_KEY` | 空 | 后端对话密钥。 |
| `EMBEDDING_MODEL_NAME` | `qwen3.7-text-embedding-flash` | Embedding 模型。后端与 searcher 必须使用同一模型。 |
| `EMBEDDING_MODEL_API_KEY` | 空 | Embedding 密钥。 |
| `EMBEDDING_MODEL_BASE_URL_WITH_OPENAI` | DashScope 兼容 URL | 必须是 OpenAI 兼容的 base，而不是 DashScope 原生 API。 |

后端对话客户端在 `backend/pkg/llm.go` 中硬编码为 `https://api.deepseek.com/chat/completions`。`MODEL_NAME` 不会改变该端点。如果你使用其他服务商，请先添加 base URL/认证适配层，再更换密钥。

### 邮件（阿里云 DirectMail）

| 变量 | 默认值 | 含义 |
| --- | --- | --- |
| `ALIYUN_ACCESS_KEY_ID` | 空 | 具备 DirectMail 发送权限的 RAM AccessKey。 |
| `ALIYUN_ACCESS_KEY_SECRET` | 空 | RAM 密钥。 |
| `MAIL_ACCOUNT_NAME` | 示例中为 `litstream@email.nexel.dpdns.org` | 已验证的发信地址。 |
| `ALIYUN_EMAIL` | 空 | 在 Go 的 env 结构体中声明，但当前邮件链路未使用。 |
| `ALIYUN_FROM` | 空 | 已声明，但当前邮件链路未使用。 |

如果邮件配置有误，注册和重置密码会在验证码这一步失败。已有账号仍然可以登录。

## 3. 参考 searcher 变量

这些变量仅由可选的 `searcher/` 参考生产者使用。如果你用自己的管道载入物品，可以忽略本节。searcher 复用同一份 `.env`，并读取 `SUMMARY_MODEL_*`、翻译设置和并发设置。

### 摘要生成

| 变量 | 默认值 | 含义 |
| --- | --- | --- |
| `SUMMARY_MODEL_NAME` | 示例中为 `qwen3.8-flash` | 摘要模型。 |
| `SUMMARY_MODEL_API_KEY` | 空 | 摘要密钥。若缺失，searcher 回退到 `MODEL_NAME` / `MODEL_API_KEY`。 |
| `ENRICH_CONCURRENCY` | 未设置/为 0 时 `8`；`.env.example` 中为 `64` | 并发处理的论文数。 |
| `SUMMARY_CONCURRENCY` | 未设置/为 0 时 `8`；`.env.example` 中为 `64` | 并发摘要请求数。 |

摘要端点在 `searcher/llm/client.go` 中硬编码为 Qwen MaaS 的 OpenAI 兼容 URL。回退配置里的 DeepSeek 密钥会被发往该 Qwen 端点并失败；此时 searcher 会回退为把原始摘要用作 `items.summary`，因此入库仍会成功，但 embedding 质量会下降。

### 翻译

| 变量 | 默认值 | 含义 |
| --- | --- | --- |
| `TRANSLATE_PROVIDER` | 空 → `alimt` | `alimt`（阿里云机器翻译）或 `qwenmt`（Qwen-MT）。模板中留空；如需 Qwen-MT，请设为 `qwenmt` 并填写 `QWEN_MT_API_KEY`。 |
| `ALIYUN_MT_ACCESS_KEY_ID` | 空 | **searcher 的** alimt AccessKey ID。与邮件所用的 `ALIYUN_ACCESS_KEY_*` 那一对相互独立。 |
| `ALIYUN_MT_ACCESS_KEY_SECRET` | 空 | searcher 的 alimt 密钥。 |
| `QWEN_MT_API_KEY` | 空 | Qwen-MT 使用的 DashScope 密钥。当 `TRANSLATE_PROVIDER=qwenmt` 时必需。 |
| `QWEN_MT_MODEL` | 空 → `qwen-mt-lite` | Qwen-MT 模型。 |
| `TRANSLATE_CONCURRENCY` | 服务商默认值：alimt 为 8，qwenmt 为 2 | 并发翻译请求数。负值表示不限制。 |
| `TRANSLATE_QPS` | 服务商默认值：alimt 为 50，qwenmt 为 1 | 每秒请求数。`0` 表示"使用服务商默认值"；负值表示不限制。 |
| `TRANSLATE_BURST` | 服务商默认值：alimt 为 50，qwenmt 为 1 | 令牌桶容量。 |

为什么需要两套阿里云凭据：

- `ALIYUN_ACCESS_KEY_*` → DirectMail，后端用于发送邮件。
- `ALIYUN_MT_ACCESS_KEY_*` → 机器翻译，searcher 使用。searcher 的配置键有意带上 `MT` 后缀；旧模板在这里用了邮件密钥，导致 alimt 被跳过，只留下一条日志警告。

如果未配置翻译，searcher 会让 `title_chinese` / `abstract_chinese` 保持为空。feed 仍然可用，只是 UI 的中文切换没有内容可切。

### 并发建议

| 场景 | 建议起点 |
| --- | --- |
| 较小的首次导入（≤200 篇论文） | `ENRICH_CONCURRENCY=8`，`SUMMARY_CONCURRENCY=8` |
| Qwen-MT（`60 RPM`） | 保持 `TRANSLATE_QPS=1`、`TRANSLATE_BURST=1`、`TRANSLATE_CONCURRENCY=2` |
| 阿里云机器翻译（`QPS 50`） | `TRANSLATE_QPS=50`、`TRANSLATE_BURST=50`、`TRANSLATE_CONCURRENCY=8` |
| 频繁遇到服务商 429 | 先降低各服务商的并发，再降 QPS |

示例 `.env.example` 设置了 `ENRICH_CONCURRENCY=64` 和 `SUMMARY_CONCURRENCY=64`。这两个值比较激进，只有在服务商配额支持时才安全。建议从更小的值开始。

## 4. 前端变量

前端在构建/开发时读取 Vite 变量。这些变量是公开的；绝不要把 API 密钥放进 `VITE_*` 变量。

| 变量 | 默认值 | 含义 |
| --- | --- | --- |
| `VITE_API_PROXY_TARGET` | `vite.config.js` 中为 `http://127.0.0.1:8080` | `/api` 的 Vite 开发代理目标。 |
| `VITE_API_TARGET` | — | 同一目标的备用变量名。 |
| `VITE_PORT` | `5173` | 开发服务器端口。 |

生产环境中，请让 `/api` 在 Nginx/Caddy 之后保持同源。后端没有 CORS 中间件。

## 5. 配置变更对照表

| 变更 | 需要的操作 |
| --- | --- |
| 仅改提示词 | 编辑 `backend/llm/prompt.go`，重新构建/重启后端。 |
| 精排候选数量 | 修改 `SEARCH_TOP_K`；注意 LLM 提示词的 token 消耗。 |
| 每个窗口的物品数 | 修改 `LIMIT_TOP_K`。 |
| 画像更新频率 | "每 5 次缓存命中更新一次"的常量在 `RecommendCache.Count` 中；需改代码。 |
| Embedding 模型 / 维度 | 设置新的 `EMBEDDING_MODEL_NAME` + `VEC_DIM`，重建 Milvus collection，重新为所有物品做 embedding，使 `vec:user:*` 失效。 |
| Collection 名称 | 修改 `MILVUS_COLLECTION_NAME`，重启后端与 searcher；向新 collection 灌数据。 |
| 翻译服务商 | 修改 `TRANSLATE_PROVIDER` 及对应凭据；重新运行 `regen-translate`。 |
| 发信地址 | 修改 `MAIL_ACCOUNT_NAME`，并在 DirectMail 中验证该地址/域名。 |
| 公网域名 / TLS | 在反向代理处终止 TLS；保持 `/api` 代理到 `127.0.0.1:8080`。 |

## 6. 密钥与安全

- `.env.example` 是模板，绝不能包含真实密钥。
- 任何曾被提交或出现在截图中的密钥都要轮换。
- DeepSeek 密钥、DashScope 密钥、DirectMail AccessKey 以及（如使用）MT AccessKey 应各自独立。它们属于不同服务，权限也不同。
- 将 DirectMail 的 RAM 用户限制为仅能发信；不要复用管理员 AccessKey。
- 修改 `VEC_DIM`、collection 名称或执行 `purge` 之前，请先备份数据库和 Milvus。
