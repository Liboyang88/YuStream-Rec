# Getting started

[English](getting-started.md) · [中文](getting-started.zh-CN.md)

This guide takes you from a clean checkout to a working LitStream feed. The fastest path is Docker Compose for the data layer and backend, plus the Vite dev server for the frontend.

> `searcher/` is the reference arXiv producer, used below to seed a demo catalog. It is **not required**: the backend only reads MySQL and Milvus, and you can load your own items with any tool. See [Data pipeline](data-pipeline.md) for the contract.

## 1. Prerequisites

| Requirement | Notes |
| --- | --- |
| Docker Desktop / Docker Engine + Compose v2 | `docker compose version` should work |
| 4 GB free RAM minimum, 8 GB recommended | Milvus standalone, MySQL, Redis, etcd, and MinIO are the memory-heavy parts |
| Node.js 18+ and Yarn 1.x (or npm) | For the Vue frontend |
| A DeepSeek API key | Used by the backend for profile generation and reranking |
| A DashScope (Alibaba Cloud Model Studio) API key | Used for embeddings; the reference searcher also uses Qwen for summaries |
| An Alibaba Cloud DirectMail sender + AccessKey | Required for email verification codes at signup |
| Optional: an Alibaba Cloud MT or Qwen-MT key | Only for Chinese translations in the reference searcher |

The backend is CPU-only. No GPU is required.

## 2. What runs where

| Process | How it runs in the quick path | Address |
| --- | --- | --- |
| MySQL, Redis, etcd, MinIO, Milvus | Docker Compose | Internal; Milvus/MinIO publish ports |
| Backend + asynq worker | Docker Compose | `http://127.0.0.1:8080` |
| Frontend dev server | Vite on the host | `http://127.0.0.1:5173` |
| Catalog ingestion | One-shot `searcher` container (reference) or your own loader | `tools` profile, or any external job |

The frontend is not part of `docker-compose.yml`. The Nginx block in that file is commented out and its referenced files are not in the repository. Use Vite for development, or serve `frontend/dist` with your own reverse proxy.

The backend has no dependency on the searcher. If you already have a catalog pipeline, skip the searcher and write the same item contract directly: a MySQL `items` row plus a Milvus `(id, vec)` with the same ID, where `vec` is the embedding of the item semantic text.

## 3. Quick path with Docker Compose

### 3.1 Create and fill `.env`

From the repository root:

```bash
# macOS / Linux
cp .env.example .env

# Windows PowerShell
Copy-Item .env.example .env
```

Then edit `.env`. At minimum, set:

| Variable | Why |
| --- | --- |
| `ACCESS_TOKEN_SECRET` | Long random string; signs access JWTs |
| `REFRESH_TOKEN_SECRET` | Long random string; signs refresh JWTs |
| `MODEL_API_KEY` | DeepSeek key for profile generation and reranking |
| `MODEL_NAME` | A model your DeepSeek key can access |
| `EMBEDDING_MODEL_API_KEY` | DashScope key for embeddings |
| `EMBEDDING_MODEL_NAME` | Must match `VEC_DIM` and the collection |
| `EMBEDDING_MODEL_BASE_URL_WITH_OPENAI` | DashScope OpenAI-compatible base URL |
| `ALIYUN_ACCESS_KEY_ID` / `ALIYUN_ACCESS_KEY_SECRET` | DirectMail credentials for signup emails |
| `MAIL_ACCOUNT_NAME` | Verified DirectMail sender address |
| `SUMMARY_MODEL_API_KEY` | Key for the Qwen MaaS endpoint used by the searcher |

Keep the Compose defaults for `MYSQL_DSN`, `REDIS_ADDRESS`, and `MILVUS_ADDRESS` unless you also change the Compose service names or passwords.

> **The Compose MySQL password is a development default.** It is hardcoded as `123456` in `docker-compose.yml` and in the sample DSN. Change both for any non-local deployment.

The full variable reference is in [Configuration](configuration.md).

### 3.2 Start the stack

```bash
docker compose up -d --build
docker compose ps
docker compose logs -f backend
```

Wait until MySQL, Redis, and Milvus report healthy. The first Milvus start can take 60–90 seconds.

Check the API:

```bash
curl http://127.0.0.1:8080/hello
# {"message":"Hello, World!"}
```

The backend mounts the root `.env` read-only at `/app/.env`; you can edit it and restart the backend without rebuilding the image:

```bash
docker compose restart backend
```

### 3.3 Seed the catalog (required)

An empty Milvus collection does not produce a friendly empty feed; recall fails and the API returns 500. You need at least one item in MySQL and a matching vector in Milvus.

The commands below use the reference arXiv searcher. If you already have a catalog pipeline, load your own items instead; the backend only requires the item contract (same ID in MySQL and Milvus, semantic text, matching embedding model/dimension).

```bash
docker compose --profile tools run --rm searcher \
  -config /app/.env \
  -date 2025-01-15 \
  -max 200
```

- `-date` uses arXiv's `submittedDate` in **US Eastern Time**.
- `-max 200` keeps the first run small; omit it to ingest the whole day.
- The searcher image is a one-shot job (`restart: "no"`). It exits when done.
- Use `-dry-run` first if you only want to see what would be fetched.

Check what landed:

```bash
docker compose --profile tools run --rm searcher \
  -config /app/.env -op stats
```

### 3.4 Start the frontend

In a second terminal:

```bash
cd frontend
yarn install
yarn dev
```

Open `http://127.0.0.1:5173`.

The Vite dev server proxies `/api` to `http://127.0.0.1:8080` (see `frontend/vite.config.js`). If you use a different backend port, set `VITE_API_PROXY_TARGET` in `frontend/.env.development`.

### 3.5 First-run checklist

1. Register with an email address; request the 6-digit code, then complete signup.
   - Password rule: 8–20 letters/digits. Username: 2–15 non-space characters.
   - The code is valid for 5 minutes; the same email cannot request another code within 60 seconds.
2. Fill in age, field, organization, and at least one interest. Submit.
3. Wait for the profile generation task. The UI polls `/api/users/info` until `summary` appears.
4. Scroll the feed to the bottom.
   - The first window is usually `source=coarse`.
   - The next window is usually `source=llm` once the async rerank finishes.
5. Open a card, like an item, and scroll a few windows. These interactions feed the incremental profile update.

## 4. Local development without the backend container

### 4.1 Data services

You need MySQL 8, Redis 7, and Milvus reachable on `localhost`. Two options:

**Option A — host-installed services.** Install MySQL and Redis locally; run Milvus standalone with its own Compose file or binary.

**Option B — reuse the Compose data layer and publish ports.** Create a temporary `docker-compose.override.yml`:

```yaml
services:
  mysql:
    ports:
      - "127.0.0.1:3306:3306"
  redis:
    ports:
      - "127.0.0.1:6379:6379"
```

Then:

```bash
docker compose up -d mysql redis etcd minio milvus
docker compose stop backend   # free port 8080 for the local Go process
```

### 4.2 Backend

Create `backend/.env` with local addresses. Do not commit it.

```dotenv
SERVER_ADDRESS=:8080
MYSQL_DSN=new_user:123456@tcp(127.0.0.1:3306)/litstream?charset=utf8mb4&parseTime=True&loc=Asia%2FShanghai&interpolateParams=true
REDIS_ADDRESS=127.0.0.1:6379
MILVUS_ADDRESS=127.0.0.1:19530
# ... plus the same model, embedding, and mail keys as the root .env
```

The database must exist before the backend starts:

```sql
CREATE DATABASE litstream CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

Run the API and worker (same process):

```bash
cd backend
go run ./cmd
```

### 4.3 Frontend

```bash
cd frontend
yarn install
yarn dev
```

### 4.4 Searcher

The searcher reads `../backend/.env` by default, which is convenient for local development:

```bash
cd searcher
go run . -date 2025-01-15 -max 200
```

If you keep a single root `.env` instead, pass it explicitly:

```bash
go run . -config ../.env -date 2025-01-15 -max 200
```

Remember that the root `.env` uses Compose hostnames (`mysql`, `redis`, `milvus`), which do not resolve from the host. Use localhost values for host-run processes.

## 5. Production build

```bash
cd frontend
yarn install
yarn build          # outputs frontend/dist
```

Serve `dist` as a static SPA and proxy `/api` to the backend on the same origin. Example Nginx configuration:

```nginx
server {
    listen 80;
    server_name example.com;

    root /srv/yustream/dist;
    index index.html;

    location / {
        try_files $uri $uri/ /index.html;
    }

    location /api/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

Because the backend binds `127.0.0.1:8080` in Compose, the reverse proxy should run on the same host or reach it through a tunnel. Add TLS (Nginx/Caddy) and do not expose MySQL, Redis, or MinIO publicly.

## 6. Reference searcher command reference

These commands are specific to the arXiv reference producer. If you load your own catalog, ignore this section.

Run them from `searcher/` locally, or prefix with `docker compose --profile tools run --rm searcher -config /app/.env` in Docker.

| Task | Command |
| --- | --- |
| Ingest one day | `go run . -date 2025-01-15` |
| Ingest a date range (inclusive) | `go run . -from 2025-01-01 -to 2025-01-31` |
| Limit a run | `go run . -date 2025-01-15 -max 200` |
| Inspect without calling LLMs/writing | `go run . -date 2025-01-15 -dry-run` |
| Count records | `go run . -op stats` |
| List covered dates | `go run . -op dates` |
| Delete one day | `go run . -op delete-date -date 2025-01-15 -yes` |
| Purge everything | `go run . -op purge -yes` |
| Regenerate translations | `go run . -op regen-translate -date 2025-01-15 -yes` |
| Regenerate summaries | `go run . -op regen-summary -date 2025-01-15 -yes` |
| Regenerate vectors | `go run . -op regen-embedding -date 2025-01-15 -yes` |

`-yes` is required for destructive/regeneration operations; without it the command only reports the scope.

Regeneration order matters: the vector is computed from the summary, so after changing the summary model run `regen-summary` first, then `regen-embedding`.

## 7. Troubleshooting

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| `docker compose ps` shows MySQL unhealthy | Password/DSN mismatch, or first initialization still running | Check `docker compose logs mysql`; keep `MYSQL_DSN` consistent with the Compose credentials |
| Backend exits with `Can't find the file .env` | `.env` missing at the process working directory | Compose mounts root `.env`; local backend needs `backend/.env` |
| `connect: connection refused` to Milvus | Milvus not healthy yet, or wrong address | Wait for health; check `MILVUS_ADDRESS` |
| `collection not loaded` | Milvus started but the collection load has not completed | Restart backend after Milvus is healthy; the service loads idempotently |
| `embedding 维度不符` | `VEC_DIM` does not match the model/collection | Set the right dimension, recreate the collection, re-ingest |
| Feed is empty or 500 on first run | No catalog ingested | Run the searcher or your own loader; verify MySQL row and Milvus vector counts |
| `LLM API 返回 401` | Wrong backend LLM key, or a key for a different provider | Check `MODEL_API_KEY`; the backend endpoint is DeepSeek-only |
| Signup code never arrives | DirectMail sender not verified, missing permission, or rate limit | Verify `MAIL_ACCOUNT_NAME`, check the RAM policy, wait 60 s, check spam |
| Translations are missing | Translation provider/key not configured | Set `TRANSLATE_PROVIDER` and the matching credentials; see [Configuration](configuration.md) |
| Frontend requests 404 after refresh in production | SPA history mode not rewritten | Add `try_files $uri $uri/ /index.html;` |
| Browser reports CORS | Frontend and API are not same-origin | Use the Vite proxy in dev or an Nginx `/api` proxy in production |
| `yarn` not found | Yarn 1.x not installed | `npm install -g yarn`, or use `npm install` / `npm run dev` |
| `go build ./...` fails in `backend/test` | `test/` contains scratch programs, not a normal package | Build/run the app with `go build ./cmd`; run targeted tests only |

## 8. Production notes

- Change the default MySQL password and all secrets. Never commit `.env`.
- Put the API behind TLS and a reverse proxy; keep MySQL, Redis, Milvus, and MinIO off the public internet.
- Back up MySQL (users/items metadata) and Milvus (vectors). Redis is mostly reconstructible, but losing it loses queued tasks, interaction buffers, and caches.
- Set Redis `maxmemory`/eviction deliberately. Evicting queue or stream keys is not equivalent to evicting a cache.
- Watch LLM token usage: every LLM response is one rerank call, and every fifth response adds a profile update. Ingestion adds one summary (and optionally one translation) per item.
- `SEARCH_TOP_K`, `LIMIT_TOP_K`, summary length, and update cadence are the main cost/latency levers.
- The backend has no built-in metrics endpoint. Add Prometheus/OpenTelemetry before running a large deployment.
