# Configuration

[English](configuration.md) · [中文](configuration.zh-CN.md)

All runtime configuration is read from a `.env` file. The backend uses Viper with `SetConfigFile(".env")`, so the file must be in the process working directory (Compose mounts it at `/app/.env`). The searcher reads `../backend/.env` by default and accepts `-config <path>`.

Start from the redacted template:

```bash
cp .env.example .env
```

Never commit `.env`. The repository's `.gitignore` excludes it at the root and in `backend/`.

## 1. Required for a working local stack

These are the values that must be real before the first signup and first feed:

| Variable | Example | Notes |
| --- | --- | --- |
| `ACCESS_TOKEN_SECRET` | long random string | HS256 signing key. An empty value produces insecure tokens. |
| `REFRESH_TOKEN_SECRET` | long random string | Refresh token signing key; no refresh route is implemented yet. |
| `MODEL_API_KEY` | `sk-...` | Backend LLM key (DeepSeek endpoint is hardcoded). |
| `MODEL_NAME` | model name from your provider | Must be a model the key can access. The sample uses `deepseek-flash`. |
| `EMBEDDING_MODEL_API_KEY` | `sk-...` | DashScope key for embeddings. |
| `EMBEDDING_MODEL_NAME` | `qwen3.7-text-embedding-flash` | Must match `VEC_DIM` and the Milvus collection. |
| `EMBEDDING_MODEL_BASE_URL_WITH_OPENAI` | `https://dashscope.aliyuncs.com/compatible-mode/v1` | OpenAI-compatible base URL; the client appends `/embeddings`. |
| `ALIYUN_ACCESS_KEY_ID` | `LTAI...` | DirectMail AccessKey for signup/reset emails. |
| `ALIYUN_ACCESS_KEY_SECRET` | secret | DirectMail secret. |
| `MAIL_ACCOUNT_NAME` | `litstream@your-domain` | A verified DirectMail sender address. |
| `SUMMARY_MODEL_API_KEY` | `sk-...` | Searcher summary key. The summary endpoint is a Qwen MaaS endpoint, not DeepSeek. |

## 2. Backend variables

### Server and auth

| Variable | Default | Meaning |
| --- | --- | --- |
| `SERVER_ADDRESS` | `:8080` | Gin listen address. Compose publishes `127.0.0.1:8080`. |
| `ACCESS_TOKEN_SECRET` | — | Access JWT secret. |
| `REFRESH_TOKEN_SECRET` | — | Refresh JWT secret. |
| `ACCESS_TOKEN_EXPIRE_HOUR` | `1` | Access token lifetime in hours. |
| `REFRESH_TOKEN_EXPIRE_HOUR` | `72` | Refresh token lifetime in hours. The SPA stores it but does not call a refresh endpoint. |
| `LOG_LEVEL` | `info` | zerolog level (`debug`, `info`, `warn`, `error`). |

### MySQL

| Variable | Default | Meaning |
| --- | --- | --- |
| `MYSQL_DSN` | Compose sample: `new_user:123456@tcp(mysql:3306)/litstream?...` | Must match the Compose database/user/password. |
| `MYSQL_MAX_IDLE_CONNS` | `20` | `SetMaxIdleConns`. |
| `MYSQL_MAX_OPEN_CONNS` | `50` | `SetMaxOpenConns`. |
| `MYSQL_CONN_MAX_LIFETIME` | `30` | Minutes. |
| `MYSQL_CONN_MAX_IDLE_TIME` | `10` | Minutes. |

The Compose file creates the `litstream` database via `MYSQL_DATABASE`. AutoMigrate creates/updates tables but **not** the database. If you run MySQL yourself, create the database first.

The Compose MySQL credentials are hardcoded in `docker-compose.yml` (`root` / `123456`, user `new_user` / `123456`). If you change them, update `MYSQL_DSN` too.

### Redis

| Variable | Default | Meaning |
| --- | --- | --- |
| `REDIS_ADDRESS` | `redis:6379` | Redis host:port. |
| `REDIS_PASSWORD` | empty | Redis AUTH password. |
| `REDIS_DB` | `0` | Redis logical database. |
| `USER_NAME_TTL` | `72` | Hours. Despite the name, this is the `email:{email}` → user ID cache TTL. |
| `USER_INFO_TTL` | `72` | Hours. `user:{id}` profile hash and topic vector TTL. |
| `ITEM_TTL` | `168` | Hours. `item:{id}` metadata cache TTL. |
| `INTERACTION_TTL` | `336` | Hours (14 days). Interaction stream and pointer TTL. |
| `RECOMMEND_TTL` | `30` | **Minutes.** Current reranked window TTL. |
| `INTERACTION_RECORD_MAX_LEN` | `40` | Max entries retained per interaction stream. |
| `QUEUE_UNIQUE_TTL1` | `60` | Seconds. asynq uniqueness window for profile tasks. |
| `QUEUE_UNIQUE_TTL2` | `60` | Seconds. asynq uniqueness window for rerank tasks. |

### Milvus and recommendation shape

| Variable | Default | Meaning |
| --- | --- | --- |
| `MILVUS_ADDRESS` | `milvus:19530` | Milvus gRPC address. |
| `MILVUS_COLLECTION_NAME` | `litstream` | Collection name. One vertical per deployment. |
| `VEC_DIM` | `1024` | Must match the embedding model output and the collection. |
| `SEARCH_TOP_K` | `100` | Candidates retrieved from Milvus and passed to the reranker. |
| `LIMIT_TOP_K` | `18` | Items the reranker is asked to select and the max returned per LLM window. |
| `RECS_WINDOW` | `10` | Anti-repeat window in **rerank rounds**, not items. With `LIMIT_TOP_K=18`, up to ~180 items can be excluded. |

### LLM and embedding

| Variable | Default | Meaning |
| --- | --- | --- |
| `MODEL_NAME` | `deepseek-flash` in the sample | Backend chat model. |
| `MODEL_API_KEY` | empty | Backend chat key. |
| `EMBEDDING_MODEL_NAME` | `qwen3.7-text-embedding-flash` | Embedding model. The backend and searcher must use the same model. |
| `EMBEDDING_MODEL_API_KEY` | empty | Embedding key. |
| `EMBEDDING_MODEL_BASE_URL_WITH_OPENAI` | DashScope compatible URL | Must be the OpenAI-compatible base, not the native DashScope API. |

The backend chat client is hardcoded to `https://api.deepseek.com/chat/completions` in `backend/pkg/llm.go`. `MODEL_NAME` does not change the endpoint. If you use another provider, add a base-URL/auth adapter before changing the key.

### Email (Alibaba Cloud DirectMail)

| Variable | Default | Meaning |
| --- | --- | --- |
| `ALIYUN_ACCESS_KEY_ID` | empty | RAM AccessKey with DirectMail send permission. |
| `ALIYUN_ACCESS_KEY_SECRET` | empty | RAM secret. |
| `MAIL_ACCOUNT_NAME` | `litstream@email.nexel.dpdns.org` in the sample | Verified sender address. |
| `ALIYUN_EMAIL` | empty | Declared in the Go env struct but unused by the current mail path. |
| `ALIYUN_FROM` | empty | Declared but unused by the current mail path. |

If mail is misconfigured, registration and password reset fail at the verification-code step. You can still log in with an existing account.

## 3. Reference searcher variables

These variables are only used by the optional `searcher/` reference producer. If you load your catalog with your own pipeline, you can ignore this section. The searcher reuses the same `.env` and reads `SUMMARY_MODEL_*`, translation settings, and concurrency settings.

### Summary generation

| Variable | Default | Meaning |
| --- | --- | --- |
| `SUMMARY_MODEL_NAME` | `qwen3.8-flash` in the sample | Summary model. |
| `SUMMARY_MODEL_API_KEY` | empty | Summary key. If missing, the searcher falls back to `MODEL_NAME` / `MODEL_API_KEY`. |
| `ENRICH_CONCURRENCY` | `8` if unset/0; `64` in `.env.example` | Papers processed concurrently. |
| `SUMMARY_CONCURRENCY` | `8` if unset/0; `64` in `.env.example` | Concurrent summary requests. |

The summary endpoint is hardcoded to a Qwen MaaS OpenAI-compatible URL in `searcher/llm/client.go`. A DeepSeek key in the fallback will be sent to that Qwen endpoint and fail; the searcher then falls back to the original abstract as `items.summary`, so ingestion still succeeds but embedding quality drops.

### Translation

| Variable | Default | Meaning |
| --- | --- | --- |
| `TRANSLATE_PROVIDER` | empty → `alimt` | `alimt` (Alibaba Cloud Machine Translation) or `qwenmt` (Qwen-MT). The template leaves it empty; set `qwenmt` and fill `QWEN_MT_API_KEY` if you want Qwen-MT. |
| `ALIYUN_MT_ACCESS_KEY_ID` | empty | **Searcher's** alimt AccessKey ID. Separate from the mail `ALIYUN_ACCESS_KEY_*` pair. |
| `ALIYUN_MT_ACCESS_KEY_SECRET` | empty | Searcher's alimt secret. |
| `QWEN_MT_API_KEY` | empty | DashScope key for Qwen-MT. Required when `TRANSLATE_PROVIDER=qwenmt`. |
| `QWEN_MT_MODEL` | empty → `qwen-mt-lite` | Qwen-MT model. |
| `TRANSLATE_CONCURRENCY` | provider default: 8 for alimt, 2 for qwenmt | Concurrent translation requests. Negative means unlimited. |
| `TRANSLATE_QPS` | provider default: 50 for alimt, 1 for qwenmt | Requests per second. `0` means "use provider default"; negative means unlimited. |
| `TRANSLATE_BURST` | provider default: 50 for alimt, 1 for qwenmt | Token bucket capacity. |

Why two Alibaba credential pairs:

- `ALIYUN_ACCESS_KEY_*` → DirectMail, used by the backend for email.
- `ALIYUN_MT_ACCESS_KEY_*` → Machine Translation, used by the searcher. The searcher's config keys intentionally carry the `MT` suffix; the older template used the mail keys here, so alimt was skipped with only a log warning.

If translation is not configured, the searcher keeps `title_chinese` / `abstract_chinese` empty. The feed still works; the UI's Chinese toggle simply has nothing to switch to.

### Concurrency guidance

| Scenario | Suggested starting point |
| --- | --- |
| Small first import (≤200 papers) | `ENRICH_CONCURRENCY=8`, `SUMMARY_CONCURRENCY=8` |
| Qwen-MT (`60 RPM`) | Keep `TRANSLATE_QPS=1`, `TRANSLATE_BURST=1`, `TRANSLATE_CONCURRENCY=2` |
| Alibaba MT (`QPS 50`) | `TRANSLATE_QPS=50`, `TRANSLATE_BURST=50`, `TRANSLATE_CONCURRENCY=8` |
| Hitting provider 429s | Lower per-provider concurrency first, then QPS |

The sample `.env.example` sets `ENRICH_CONCURRENCY=64` and `SUMMARY_CONCURRENCY=64`. These are aggressive; they are safe only if the provider quota supports them. Start lower.

## 4. Frontend variables

The frontend reads Vite variables at build/dev time. They are public; never put an API key in a `VITE_*` variable.

| Variable | Default | Meaning |
| --- | --- | --- |
| `VITE_API_PROXY_TARGET` | `http://127.0.0.1:8080` in `vite.config.js` | Vite dev proxy target for `/api`. |
| `VITE_API_TARGET` | — | Fallback name for the same target. |
| `VITE_PORT` | `5173` | Dev server port. |

For production, keep `/api` same-origin behind Nginx/Caddy. The backend has no CORS middleware.

## 5. Configuration change matrix

| Change | Required action |
| --- | --- |
| Prompt text only | Edit `backend/llm/prompt.go`, rebuild/restart backend. |
| Rerank candidate count | Change `SEARCH_TOP_K`; watch LLM prompt tokens. |
| Items per window | Change `LIMIT_TOP_K`. |
| Update cadence | The "every 5 cache hits" constant is in `RecommendCache.Count`; change code. |
| Embedding model / dimension | Set new `EMBEDDING_MODEL_NAME` + `VEC_DIM`, recreate the Milvus collection, re-embed all items, invalidate `vec:user:*`. |
| Collection name | Change `MILVUS_COLLECTION_NAME`, restart backend and searcher; seed the new collection. |
| Translation provider | Change `TRANSLATE_PROVIDER` and the matching credentials; re-run `regen-translate`. |
| Mail sender | Change `MAIL_ACCOUNT_NAME` and verify the address/domain in DirectMail. |
| Public hostname / TLS | Terminate TLS at the reverse proxy; keep `/api` proxied to `127.0.0.1:8080`. |

## 6. Secrets and safety

- `.env.example` is a template and must never contain live keys.
- Rotate any key that has ever been committed or shared in a screenshot.
- Use a separate DeepSeek key, DashScope key, DirectMail AccessKey, and (if used) MT AccessKey. They are different services with different permissions.
- Restrict the DirectMail RAM user to mail sending; do not reuse an administrator AccessKey.
- Back up the database and Milvus before changing `VEC_DIM`, the collection name, or running `purge`.
