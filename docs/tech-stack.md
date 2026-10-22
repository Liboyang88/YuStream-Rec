# Tech stack

[English](tech-stack.md) · [中文](tech-stack.zh-CN.md)

YuStream deliberately uses a small, conventional stack. There is no model training framework, no feature store, no Kubernetes dependency, and no GPU requirement in the application layer.

## At a glance

| Layer | Choice | Version / image |
| --- | --- | --- |
| Frontend | Vue 3 + Vite + Pinia + Vue Router + Axios | Vue 3.5.13, Vite 6.4.3, Pinia 2.3.1, Vue Router 4.6.4, Axios 1.7.9 |
| Math rendering | KaTeX | 0.18.7 |
| Backend | Go + Gin | Go 1.26.3, Gin 1.12.0 |
| ORM | GORM + MySQL driver | GORM 1.31.1, driver 1.6.0 |
| Cache / streams / queue | Redis + go-redis + asynq | Redis 7 Alpine, go-redis 9.20.0, asynq 0.26.0 |
| Vector database | Milvus standalone + Go SDK | Milvus 2.5.8, SDK v2.4.2 |
| Relational database | MySQL | 8.0 |
| Milvus dependencies | etcd + MinIO | etcd 3.5.18, MinIO `RELEASE.2023-03-20T20-16-18Z` |
| Logging | zerolog + lumberjack | zerolog 1.35.1 |
| Config | Viper | 1.21.0 |
| Auth | JWT + bcrypt | golang-jwt/jwt v4.5.2, x/crypto |
| Validation | go-playground/validator | 10.30.1 |
| Reference offline ingestion (optional) | Separate Go module (`searcher/`) | Own `go.mod`, same Go version |
| Local orchestration | Docker Compose | Compose v2 |

## Frontend

| Concern | Implementation | Location |
| --- | --- | --- |
| Framework | Vue 3 Composition API, `<script setup>` | `frontend/src/` |
| Build/dev server | Vite 6 | `frontend/vite.config.js` |
| State | Pinia stores (`auth`, `feed`, `preferences`, `ui`) | `frontend/src/stores/` |
| Routing | Vue Router 4, history mode | `frontend/src/router/index.js` |
| HTTP | Axios instance with JWT interceptor and 401 handler | `frontend/src/api/client.js` |
| Styling | Hand-written CSS with design tokens; no UI framework | `frontend/src/styles/base.css` |
| Animation | CSS transitions + `IntersectionObserver` reveal directive | `frontend/src/directives/reveal.js` |
| Smooth wheel | Custom `requestAnimationFrame` easing | `frontend/src/composables/useSmoothWheel.js` |
| Math | KaTeX with a small fallback renderer | `frontend/src/components/MathText.vue`, `utils/mathText.js` |

The SPA is a static build (`dist/`). It expects `/api` to be same-origin: Vite proxies it in development, and a reverse proxy should do the same in production. The backend has no CORS middleware.

## Backend

| Concern | Implementation | Location |
| --- | --- | --- |
| HTTP | Gin, grouped `/api` routes, JWT middleware | `backend/api/` |
| Business orchestration | `service` layer, one use-case per method | `backend/internal/service/` |
| Data access | `repository` layer (MySQL, Redis, Milvus) | `backend/internal/repository/` |
| Async tasks | asynq producer/consumer over Redis | `backend/internal/queue/`, `backend/internal/worker/` |
| LLM prompts and calls | `llm` package, forced tool calls | `backend/llm/` |
| Provider client | Minimal OpenAI-style chat client | `backend/pkg/llm.go` |
| Embeddings | `go-openai` client pointed at DashScope-compatible endpoint | `backend/internal/util/embedutil/` |
| Email | Alibaba Cloud DirectMail SDK | `backend/internal/util/mailutil/` |
| Rate limiting | Redis Lua fixed-window limiter | `backend/api/middleware/rate_limit_middleware.go` |
| Logging | zerolog with request IDs; GORM/asynq adapters | `backend/pkg/mylog/` |
| Startup wiring | Single composition root | `backend/bootstrap/app.go` |

The HTTP server and the asynq worker run in the same process. `bootstrap.StartServer` starts the worker in a goroutine and then `gin.Run`. asynq is configured with concurrency 50 and up to 3 retries per task.

## Data layer

### MySQL 8.0

- `users` and `items` are created/updated with GORM `AutoMigrate` at startup.
- The database itself is **not** created by AutoMigrate. Docker Compose creates `litstream` via `MYSQL_DATABASE`; a manual setup must create it first.
- `users.email` is unique. `items.url` is effectively the unique catalog identifier (arXiv ID for LitStream), though it is not a database unique index.
- Connection pool settings are configurable: idle 20, open 50, lifetime 30 min, idle time 10 min by default.

### Redis 7

Redis carries four different kinds of state:

1. **Caches**: profile hash, item JSON, topic vector.
2. **Interaction log**: one Stream per user plus a pointer key.
3. **Recommendation state**: current reranked window, window counter, recent-recommendation ZSET.
4. **Task queue**: asynq lists/queues.

AOF persistence is enabled in Compose (`--appendonly yes`) so queued tasks survive a restart.

### Milvus 2.5.8

- Standalone mode with etcd for metadata and MinIO for object storage.
- One collection (`MILVUS_COLLECTION_NAME`), schema `id: Int64` + `vec: FloatVector(VEC_DIM)`.
- HNSW index, L2 metric, `M=16`, `efConstruction=64`.
- The collection is loaded at startup. The searcher ensures the same schema and index when it writes.

## AI services

| Purpose | Default | Configurable? |
| --- | --- | --- |
| Profile generation/update, reranking | DeepSeek chat completions (`https://api.deepseek.com/chat/completions`) | Model and API key via `.env`; **endpoint is hardcoded** in `backend/pkg/llm.go` |
| Item summary generation (offline) | Qwen `qwen3.8-flash` via a Qwen MaaS OpenAI-compatible endpoint | Model/key via `SUMMARY_MODEL_*`; **endpoint is hardcoded** in `searcher/llm/client.go` |
| Embeddings | `qwen3.7-text-embedding-flash` via DashScope OpenAI-compatible endpoint | Name, key, and base URL via `EMBEDDING_MODEL_*`; dimension via `VEC_DIM` |
| Chinese translation | Qwen-MT (`qwen-mt-lite`) or Alibaba Cloud Machine Translation | `TRANSLATE_PROVIDER` plus provider credentials |

There is no local inference. Every LLM, embedding, and translation call is a network request. The application layer can run on a CPU-only host; the memory cost is dominated by Milvus, MySQL, and Redis.

## Reference offline pipeline (`searcher/`, optional)

The searcher is a separate Go module with its own Dockerfile and no import dependency on the backend. It shares the same `.env` and writes to the same MySQL/Milvus. It is one concrete producer, not a required part of the runtime: any pipeline that writes the item contract (`raw → semantic text → vector`, same ID in MySQL/Milvus) is compatible.

| Package | Responsibility |
| --- | --- |
| `arxiv/` | arXiv Atom API client: pagination, retry, IP fallback, XML parsing |
| `llm/` | Summary generation client |
| `translator/` | `alimt` and `qwenmt` translation providers |
| `embedder/` | Batched embeddings (batch size 10) |
| `ingest/` | Fetch → map → dedupe → enrich → embed → store |
| `store/` | MySQL + Milvus writes, dedupe queries, maintenance |
| `regen/` | Re-generate translation/summary/vector for a date |
| `maintain/` | `stats`, `dates`, `delete-date`, `purge` |
| `semaphore/` | Per-provider concurrency gate and rate limiter |
| `retry/` | Backoff retries with non-retryable error marking |

## Deployment and network

Docker Compose services and host ports:

| Service | Container | Host port | Notes |
| --- | --- | --- | --- |
| MySQL | `litstream-mysql` | internal 3306 | Not published to the host by default |
| Redis | `litstream-redis` | internal 6379 | Not published to the host by default |
| etcd | `milvus-etcd` | internal 2379 | Milvus metadata |
| MinIO | `milvus-minio` | `9000`, `9001` | Object storage and console |
| Milvus | `milvus-standalone` | `19530`, `9091` | gRPC and health/metrics |
| Backend | `litstream-backend` | `127.0.0.1:8080` | Bound to loopback only |
| searcher | `litstream-searcher` | none | `tools` profile, one-shot job |

The frontend is **not** part of the Compose file. The Nginx service is commented out and its referenced Dockerfile/config are not present. Serve `frontend/dist` with your own Nginx/Caddy, or use `yarn dev` locally. The backend binds to loopback, so a reverse proxy (or an frp tunnel, as in the demo deployment) must sit in front of it for public access.

## Version compatibility notes

- `go.mod` declares Go 1.26.3; the Docker builder uses `golang:1.26`.
- The Milvus Go SDK is v2.4.2 while the server is v2.5.8. The subset used here (collection create/load, HNSW index, insert/search/query) works, but do not assume newer 2.5-only APIs are available through the SDK.
- `VEC_DIM` must match the embedding model output and the existing Milvus collection. Changing it requires a new collection and a full re-embed.
- Node.js 18+ is required by the frontend. The repository includes `yarn.lock`; use Yarn 1.x for the closest reproduction, or npm if you accept a different lockfile.

## What is intentionally absent

- TypeScript, SSR, and a component library.
- A separate model-serving or feature-serving service.
- A universal ingestion connector. `searcher/` is an arXiv reference; your loader can be anything.
- Kafka/RabbitMQ (Redis + asynq is enough for this scale).
- CORS (same-origin proxy is assumed).
- A prompt registry or hot-reload mechanism.
- A built-in evaluation harness or benchmark suite.

These omissions keep the deployment small; they are also the first things to add if the project moves beyond a prototype.
