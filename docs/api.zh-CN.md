# HTTP API

[English](api.md) · [中文](api.zh-CN.md)

基础路径：`/api`（另有 `GET /hello` 用于健康检查）。SPA 在同源代理后使用相对 URL。

## 1. 认证

受保护的端点需要携带：

```http
Authorization: Bearer <accessToken>
```

访问令牌是 HS256 JWT，包含用户 ID 和邮箱，在 `ACCESS_TOKEN_EXPIRE_HOUR` 之后过期（默认 1 小时）。登录/注册的响应还会返回 refresh token，但**目前尚未实现 refresh 端点**；SPA 会存储它但并不使用。

## 2. 错误格式

业务错误：

```json
{ "kind": "Register", "msg": "该邮箱已关联账号" }
```

校验错误可能把字段映射放进 `msg`：

```json
{ "kind": "http request", "msg": { "Email": "邮箱格式错误" } }
```

| 状态码 | 含义 |
| --- | --- |
| `200` | 成功 |
| `201` | 已创建（注册、交互） |
| `202` | 已接受（异步画像任务已入队） |
| `400` | 请求无效 / 校验失败 |
| `401` | 缺失/无效/过期的 token |
| `404` | 资源不存在 |
| `409` | 冲突（邮箱已被占用、重发过快） |
| `429` | 触发限流；查看 `Retry-After` |
| `500` | 服务端或下游失败 |

## 3. 端点概览

### 公开

| 方法 | 路径 | 用途 | 成功状态码 |
| --- | --- | --- | --- |
| `GET` | `/hello` | 健康检查 | `200` |
| `POST` | `/api/users/signup/email` | 发送注册验证码 | `202` |
| `POST` | `/api/users/signup` | 完成注册，签发 token | `201` |
| `POST` | `/api/users/login` | 邮箱 + 密码登录 | `200` |
| `POST` | `/api/users/forget-password/email` | 发送重置密码验证码 | `202` |
| `PATCH` | `/api/users/forget-password` | 用验证码重置密码 | `200` |

### 私有

| 方法 | 路径 | 用途 | 成功状态码 |
| --- | --- | --- | --- |
| `GET` | `/api/users/info` | 读取画像 + summary | `200` |
| `POST` | `/api/users/info` | 保存画像字段（不触发画像任务） | `200` |
| `PUT` | `/api/users/info` | 更新字段并投递画像更新任务 | `200` |
| `PUT` | `/api/users/summary` | 投递全量画像重生成任务 | `202` |
| `POST` | `/api/users/summary` | 投递增量画像更新任务 | `202` |
| `GET` | `/api/items/recommend` | 获取一个推荐窗口 | `200` |
| `POST` | `/api/interactions` | 记录一次交互 | `201` |

## 4. 认证端点

### `POST /api/users/signup/email`

限流：每个 IP 每天 10 次请求（验证邮件限流器）。请求体：

```json
{
  "username": "alice",
  "email": "alice@example.com",
  "password": "pass1234",
  "repassword": "pass1234"
}
```

尽管只有邮箱会被用于发送验证码，用户名/密码仍会校验。响应：

```json
{ "kind": "Send Email", "msg": "验证码已发送，请注意查收" }
```

验证码为 6 位数字，有效期 5 分钟。同一邮箱 60 秒内不能再次请求验证码。

### `POST /api/users/signup`

```json
{
  "username": "alice",
  "email": "alice@example.com",
  "password": "pass1234",
  "code": "123456"
}
```

规则：用户名 2–15 个非空格字符；密码 8–20 位字母/数字；邮箱格式有效；验证码 6 位数字。响应 `201`：

```json
{
  "accessToken": "eyJ...",
  "refreshToken": "eyJ..."
}
```

### `POST /api/users/login`

限流：每个 IP 每分钟 10 次请求。

```json
{ "email": "alice@example.com", "password": "pass1234" }
```

响应 `200`，返回相同结构的两枚 token。

### `POST /api/users/forget-password/email`

```json
{ "email": "alice@example.com", "password": "newpass123", "repassword": "newpass123" }
```

先确认邮箱存在，然后发送验证码。

### `PATCH /api/users/forget-password`

```json
{ "email": "alice@example.com", "password": "newpass123", "repassword": "newpass123", "code": "123456" }
```

不签发 token；请用新密码重新登录。

## 5. 画像端点

### `GET /api/users/info`

```json
{
  "age": 24,
  "gender": "female",
  "job": "graduate student",
  "education": "master",
  "location": "Shanghai",
  "organization": "Example University",
  "interestingFields": ["recommender systems", "LLM"],
  "summary": "The user is a graduate student working on..."
}
```

在异步画像任务完成之前，`summary` 为空。`topics` 有意不在该响应中暴露；它属于内部召回状态。

### `POST /api/users/info`（首次引导）

部分更新。只写入请求中提供的字段；至少需要提供一个字段。该端点**不会**投递画像任务。引导流程的 UI 随后会调用 `PUT /api/users/summary`。

```json
{
  "age": 24,
  "gender": "female",
  "job": "graduate student",
  "education": "master",
  "location": "Shanghai",
  "organization": "Example University",
  "interesting_fields": ["recommender systems", "LLM"]
}
```

校验：age 0–120；gender 为 `male`/`female`；文本字段 1–40；`interesting_fields` 1–10 项。

### `PUT /api/users/info`（编辑）

请求体与 `POST` 相同，但后端会写入字段并投递 `user_summary:new_info`，依据变更后的字段更新画像。响应立即返回；客户端需轮询 `GET /api/users/info`。

### `PUT /api/users/summary`

无请求体。投递一次全量画像重生成（`user_summary:generate`）。返回 `202`：

```json
{ "kind": "EnqueueProfileRegeneration", "msg": "提交`生成用户画像`任务成功" }
```

### `POST /api/users/summary`

无请求体。依据交互流投递一次增量画像更新（`user_summary:update`）。返回 `202`。

画像处理是异步的。请轮询 `GET /api/users/info`，直到 `summary` 出现/发生变化。SPA 每 2.5 秒轮询一次，最多持续 45 秒。

## 6. 推荐端点

### `GET /api/items/recommend`

无路径/查询参数。每次请求返回**一个窗口**；没有游标，也没有页码。

```json
{
  "items": [
    {
      "id": 42,
      "title": "A Paper Title",
      "web_source": "arxiv",
      "url": "http://arxiv.org/abs/2406.00001v1",
      "publish_date": "2025-01-15",
      "authors": ["A. Author", "B. Author"],
      "abstract": "Original abstract...",
      "fields": ["cs.IR", "cs.CL"],
      "keywords": [],
      "is_chinese": false,
      "title_chinese": "中文标题",
      "abstract_chinese": "中文摘要...",
      "summary": "Enriched, embedding-ready summary..."
    }
  ],
  "source": "llm",
  "outline": "This window focuses on retrieval-augmented..."
}
```

`source` 的取值：

| 取值 | 含义 |
| --- | --- |
| `llm` | 来自缓存的 LLM 精排窗口 |
| `coarse` | 仅向量召回，不等待 LLM |
| `empty` | 画像尚未就绪；API 同时会投递画像生成任务 |

`outline` 是 LLM 对该窗口的描述。粗排窗口的 `outline` 为空。

行为细节：

- 缓存命中时返回缓存窗口，**并**投递下一次精排。
- 每第 5 个 `llm` 响应会投递一次增量画像更新。
- 缓存未命中时同步执行召回（嵌入 + Milvus + 物品加载），返回 `coarse`，并投递一次精排。
- 画像缺失时，端点返回 `200`，`items: []`、`source: "empty"`，并投递画像生成任务。
- Milvus 不可用或 collection 为空时，召回会报错，端点返回 `500`。
- 服务端并不知道客户端已经渲染了哪些物品；SPA 按 `id` 去重。
- API 不会触发目录数据入库（catalog ingestion）。物品来自任何填充了 MySQL 和 Milvus 的管线（`searcher/` 只是其中一种参考实现）。

## 7. 交互端点

### `POST /api/interactions`

```json
{
  "type": "click",
  "target": {
    "id": 42,
    "title": "A Paper Title",
    "abstract": "...",
    "summary": "..."
  },
  "detail": "card",
  "long_ts": 0
}
```

| 类型 | `target` | `detail` | `long_ts` |
| --- | --- | --- | --- |
| `click` | 必填 | `card` 或 `link` | 0 |
| `scroll` | 省略/为空 | 窗口的 `outline` | 秒数，上限 300 |
| `like` | 必填 | 为空 | 0 |
| `view` | 必填 | 为空 | 秒数，上限 180 |

`detail` 可选，最多 300 个字符。后端会把事件追加到该用户的 Redis Stream 并返回 `201`：

```json
{ "kind": "Record Interaction", "msg": "RecordInteraction success" }
```

在 SPA 中交互是发后即忘的；请求失败不会中断阅读。

## 8. 限流

| 端点 | 限制 |
| --- | --- |
| `POST /api/users/login` | 每个 IP 10 次/分钟 |
| `POST /api/users/signup` | 每个 IP 5 次/分钟 |
| `PATCH /api/users/forget-password` | 每个 IP 每 5 分钟 100 次 |
| `POST /api/users/signup/email` | 每个 IP 每天 10 次 |
| `POST /api/users/forget-password/email` | 每个 IP 每天 10 次 |

限流基于 Redis 固定窗口。如果 Redis 故障，限流器会失效放行（fail open）并记录错误。被限流的请求返回 `429`，并带上 `Retry-After`。

## 9. 客户端行为说明

- SPA 把 `401` 视为会话过期：它会清除本地 token 并跳转到 `/auth`。
- 网络故障（`status === 0`）会阻止自动加载 feed，直到用户重试；普通的 `coarse`/空窗口不会。
- 登录基于邮箱；用户名仅用于展示。
- 没有 refresh token 调用。访问令牌过期后，用户需要重新登录。
