# HTTP API

[English](api.md) · [中文](api.zh-CN.md)

Base path: `/api` (plus `GET /hello` for health). The SPA uses relative URLs behind a same-origin proxy.

## 1. Authentication

Protected endpoints require:

```http
Authorization: Bearer <accessToken>
```

Access tokens are HS256 JWTs with the user ID and email, expiring after `ACCESS_TOKEN_EXPIRE_HOUR` (1 hour by default). The login/signup response also returns a refresh token, but **no refresh endpoint is implemented yet**; the SPA stores it and does not use it.

## 2. Error format

Business errors:

```json
{ "kind": "Register", "msg": "该邮箱已关联账号" }
```

Validation errors may put a field map in `msg`:

```json
{ "kind": "http request", "msg": { "Email": "邮箱格式错误" } }
```

| Status | Meaning |
| --- | --- |
| `200` | Success |
| `201` | Created (signup, interaction) |
| `202` | Accepted (async profile task enqueued) |
| `400` | Invalid request / validation |
| `401` | Missing/invalid/expired token |
| `404` | Resource not found |
| `409` | Conflict (email taken, resend too soon) |
| `429` | Rate limited; check `Retry-After` |
| `500` | Server or downstream failure |

## 3. Endpoint summary

### Public

| Method | Path | Purpose | Success |
| --- | --- | --- | --- |
| `GET` | `/hello` | Health check | `200` |
| `POST` | `/api/users/signup/email` | Send signup verification code | `202` |
| `POST` | `/api/users/signup` | Complete signup, issue tokens | `201` |
| `POST` | `/api/users/login` | Email + password login | `200` |
| `POST` | `/api/users/forget-password/email` | Send password-reset code | `202` |
| `PATCH` | `/api/users/forget-password` | Reset password with code | `200` |

### Private

| Method | Path | Purpose | Success |
| --- | --- | --- | --- |
| `GET` | `/api/users/info` | Read profile + summary | `200` |
| `POST` | `/api/users/info` | Save profile fields (no profile task) | `200` |
| `PUT` | `/api/users/info` | Update fields + enqueue profile update | `200` |
| `PUT` | `/api/users/summary` | Enqueue full profile regeneration | `202` |
| `POST` | `/api/users/summary` | Enqueue incremental profile update | `202` |
| `GET` | `/api/items/recommend` | Get one recommendation window | `200` |
| `POST` | `/api/interactions` | Record one interaction | `201` |

## 4. Auth endpoints

### `POST /api/users/signup/email`

Rate limit: 10 requests/day per IP (the verification-email limiter). Body:

```json
{
  "username": "alice",
  "email": "alice@example.com",
  "password": "pass1234",
  "repassword": "pass1234"
}
```

The username/password are validated even though only the email is used for the code. Response:

```json
{ "kind": "Send Email", "msg": "验证码已发送，请注意查收" }
```

The code is 6 digits, valid for 5 minutes. The same email cannot request another code for 60 seconds.

### `POST /api/users/signup`

```json
{
  "username": "alice",
  "email": "alice@example.com",
  "password": "pass1234",
  "code": "123456"
}
```

Rules: username 2–15 non-space characters; password 8–20 letters/digits; email valid; code 6 digits. Response `201`:

```json
{
  "accessToken": "eyJ...",
  "refreshToken": "eyJ..."
}
```

### `POST /api/users/login`

Rate limit: 10 requests/min per IP.

```json
{ "email": "alice@example.com", "password": "pass1234" }
```

Response `200` with the same token shape.

### `POST /api/users/forget-password/email`

```json
{ "email": "alice@example.com", "password": "newpass123", "repassword": "newpass123" }
```

Checks that the email exists, then sends a code.

### `PATCH /api/users/forget-password`

```json
{ "email": "alice@example.com", "password": "newpass123", "repassword": "newpass123", "code": "123456" }
```

Does not issue tokens; log in again with the new password.

## 5. Profile endpoints

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

`summary` is empty until the async profile task completes. `topics` is intentionally not exposed in this response; it is internal recall state.

### `POST /api/users/info` (first onboarding)

Partial update. Only provided fields are written; at least one field is required. This endpoint **does not** enqueue a profile task. The onboarding UI then calls `PUT /api/users/summary`.

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

Validation: age 0–120; gender `male`/`female`; text fields 1–40; `interesting_fields` 1–10 items.

### `PUT /api/users/info` (edit)

Same body as `POST`, but the backend writes the fields and enqueues `user_summary:new_info` to update the profile from the changed fields. The response is immediate; the client polls `GET /api/users/info`.

### `PUT /api/users/summary`

No body. Enqueues a full profile regeneration (`user_summary:generate`). Returns `202`:

```json
{ "kind": "EnqueueProfileRegeneration", "msg": "提交`生成用户画像`任务成功" }
```

### `POST /api/users/summary`

No body. Enqueues an incremental profile update from the interaction stream (`user_summary:update`). Returns `202`.

Profile work is asynchronous. Poll `GET /api/users/info` until `summary` appears/changes. The SPA polls every 2.5 s for up to 45 s.

## 6. Recommendation endpoint

### `GET /api/items/recommend`

No path/query parameters. Every request returns **one window**; there is no cursor or page number.

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

`source` values:

| Value | Meaning |
| --- | --- |
| `llm` | Served from the cached LLM-reranked window |
| `coarse` | Vector recall only; no LLM wait |
| `empty` | Profile not ready; the API also enqueues profile generation |

`outline` is the LLM's description of the window. It is empty for coarse windows.

Behavior details:

- A cache hit returns the cached window **and** enqueues the next rerank.
- Every fifth `llm` response enqueues an incremental profile update.
- A cache miss runs recall synchronously (embedding + Milvus + item load), returns `coarse`, and enqueues a rerank.
- If the profile is missing, the endpoint returns `200` with `items: []`, `source: "empty"`, and enqueues profile generation.
- If Milvus is unavailable or the collection is empty, recall errors and the endpoint returns `500`.
- The server does not know which items the client has already rendered; the SPA de-duplicates by `id`.
- The API does not trigger catalog ingestion. Items come from whatever pipeline populated MySQL and Milvus (`searcher/` is only one reference).

## 7. Interaction endpoint

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

| Type | `target` | `detail` | `long_ts` |
| --- | --- | --- | --- |
| `click` | Required | `card` or `link` | 0 |
| `scroll` | Omit/empty | Window `outline` | Seconds, capped at 300 |
| `like` | Required | Empty | 0 |
| `view` | Required | Empty | Seconds, capped at 180 |

`detail` is optional and limited to 300 characters. The backend appends the event to the user's Redis Stream and returns `201`:

```json
{ "kind": "Record Interaction", "msg": "RecordInteraction success" }
```

Interactions are fire-and-forget in the SPA; a failed request does not interrupt reading.

## 8. Rate limits

| Endpoint | Limit |
| --- | --- |
| `POST /api/users/login` | 10/min per IP |
| `POST /api/users/signup` | 5/min per IP |
| `PATCH /api/users/forget-password` | 100 per 5 min per IP |
| `POST /api/users/signup/email` | 10 per day per IP |
| `POST /api/users/forget-password/email` | 10 per day per IP |

Rate limiting is a Redis fixed window. If Redis fails, the limiter fails open and logs the error. A limited request returns `429` with `Retry-After`.

## 9. Client behavior notes

- The SPA treats `401` as a session expiry: it clears local tokens and redirects to `/auth`.
- A network failure (`status === 0`) blocks automatic feed loading until the user retries. A normal `coarse`/empty window does not.
- Login is email-based; the username is display-only.
- There is no refresh-token call. When the access token expires, the user logs in again.
