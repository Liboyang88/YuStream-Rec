<div align="center">

# YuStream

**Prompt-first, vector+LLM-driven streaming recommendations for vertical products.**

Reference application: **LitStream** — an arXiv paper recommender.

[Online LitStream](https://nexel.dpdns.org) | 
[Docs](docs/README.md) | 
[License](LICENSE) |
[简体中文](README.zh-CN.md)

</div>

> YuStream is an LLM-API-driven recommendation framework for small and medium vertical catalogs. Instead of building a feature pipeline, training a ranking model, and operating a serving stack, you describe the domain in prompts, embed the catalog, and let an LLM maintain a user profile and rerank a stream of candidates.
>
> LitStream is the reference implementation in this repository: a feed of arXiv papers. It uses Milvus for recall, DeepSeek for profile/rerank calls, and DashScope embeddings.

**Status:** research and engineering prototype. The loop, auth, ingestion, and Docker setup work. Performance and cost benchmarks are not published yet — see [§4](#4-performance).

---

## Contents

1. [What is YuStream?](#1-what-is-yustream)
   - [1.1 Highlights](#11-highlights)
   - [1.2 How it compares](#12-how-it-compares)
2. [How YuStream works](#2-how-yustream-works)
3. [Quick start](#3-quick-start)
   - [3.1 Try LitStream online](#31-try-litstream-online)
   - [3.2 Run it with Docker Compose](#32-run-it-with-docker-compose)
4. [Performance](#4-performance)
5. [Other](#5-other)

---

## 1. What is YuStream?

YuStream is a **framework plus a working reference app** for vertical recommendation. Its core loop is:

```text
basic user info ──LLM──> profile (summary + interest topics)
                              │
                       embed topics
                              │
                     Milvus vector recall
                              │
                  filter recently recommended
                              │
             LLM rerank / direct coarse return
                              │
                         feed window
                              │
                 interactions (click, scroll, like, view)
                              │
                  every N windows: LLM profile update
                              └──────────────► profile
```

There is no model training and no feature store. The “query” is a maintained user profile; the “document” is a text summary of each item. That is what makes the system cheap to stand up and easy to move between verticals.

### Who it is for

- Small and medium vertical catalogs where a dedicated recsys team is not economical.
- Products with little or no interaction history at launch (cold start).
- Domains where item text is rich and semantics matter: papers, courses, jobs, products, events, media, internal knowledge.
- Teams that expect to change the recommendation logic often and want to iterate in prompts rather than retrain models.

<!-- ### Who it is not for

- Web-scale catalogs (billions of items) or sub-100 ms p99 ranking.
- Ad auctions and revenue-constrained allocation.
- Situations where a mature behavioral recommender already has abundant data and measured lift.
- Fully factual, audited decision-making without human review. The LLM is constrained to supplied candidates, but prompts are not a correctness proof. -->

### 1.1 Highlights

**1. Low cost to build and run**

- No feature engineering pipeline, training job, model registry, or serving cluster.
- One Go service (HTTP + async worker in the same process) and a static Vue app.
- LLM calls are asynchronous and hidden behind a one-window-ahead cache; a request never blocks on an LLM.
- Designed to run on a single host: the app layer is a small Go binary plus static assets. The memory floor is set by Milvus/MySQL/Redis, not by an ML runtime.
- Coarse vector recall is a built-in fallback when the LLM is slow, unavailable, or the cache is cold.

**2. Low barrier to reuse**

- Domain behavior is concentrated in prompts and the item schema.
- The reference pipeline is deliberately split into a reusable `searcher/` module: swap arXiv for your catalog source, keep the rest.
- A new vertical normally needs: a source adapter, an item mapping, a prompt rewrite, an embedding model/dimension choice, and UI copy.
- If your items fit the existing `title + abstract/description + tags + summary` shape, you can reuse the schema unchanged.

**3. Semantic and cold-start friendly**

- Recall is driven by embeddings, not by historical co-occurrence.
- A new user gets recommendations after filling a short profile — no interaction history required.
- The profile separates a long-term, readable `summary` from short-term `interest_topics`; only the topics are embedded, which keeps recall focused.
- The reranker sees the profile and reasons about the inferred information need, not just keyword similarity.

**4. A different interaction model**

- No “ask the recommender a question” step. The system infers the need from the profile and behavior.
- The feed is an endless sequence of windows. Users scroll; the system observes; the profile evolves.
- Each LLM-ranked window carries an `outline` explaining what the set is about, so the recommendation is inspectable.

### 1.2 How it compares

| Dimension | Traditional recommender | Conversational LLM recommender | YuStream |
| --- | --- | --- | --- |
| Primary input | Historical behavior, features | A user prompt each session | Maintained profile + interactions; no prompt needed |
| Cold start | Needs data, retraining, or fallbacks | Needs a conversation | Works from a text profile + embedded catalog |
| Build cost | Feature pipeline, training, serving, experimentation | Prompt + chat UI + retrieval glue | Prompt + vector DB + one small service |
| Iteration | Retrain / redeploy / re-tune | Edit prompts | Edit prompts and config; rebuild currently required |
| Latency model | Low, optimized serving path | Waits for a chat turn | Window prepared for the next scroll；no need to wait for LLM output |
| Scale sweet spot | Very large catalogs with dense data | Need for Q&A | Small/medium vertical feeds |
| Explainability | Feature attributions, offline metrics | Chat transcript | Readable profile + per-window outline |
| Failure mode | Stale features, feedback loops | User must know what to ask; prompt fatigue | Coarse fallback when the LLM is unavailable |
| Novelty | Mature engineering | Conversational interaction | Profile-maintained, push-style streaming interaction |

**Versus a traditional recommender system.** YuStream removes the most expensive parts: feature engineering, model training, and a separate serving stack. It also changes the matching from behavioral similarity to semantic fit, which helps when data is sparse. The trade-off is scale and latency: a vector search plus an LLM reranker will not match a tuned large-scale ranker at web scale, and the LLM window is intentionally one step behind the feed.

**Versus a conversational LLM recommender.** Conversational tools require the user to state a need and then answer follow-ups. YuStream maintains a profile instead, observes what the user actually reads, and pushes the next window without asking. This widens the interaction boundary of LLM recommenders: recommendation becomes a continuous feed rather than a Q&A session. The trade-off is inference risk — the system can guess wrong — which is why the profile is editable and interactions continuously correct it.

**Version adaptation.** For many changes, the domain logic to edit is a prompt, not a trained artifact. A new ranking policy, a new diversity requirement, or a new definition of “information need” can be expressed in `backend/llm/prompt.go`. Today those prompts are Go constants, so a rebuild/restart is required; there is no prompt registry yet.

---

## 2. How YuStream works

![The YuStream recommendation loop](docs/assets/recommendation-loop.svg)

### The loop in six steps

**1. Profile bootstrap.** The user submits age, gender, job, education, location, organization, and up to 10 interests. An LLM tool call returns:

- `summary` — a natural-language profile including basic facts, long-term topics, inferred information needs, and uncertain needs.
- `interest_topics` — a short, deliberately unstable list of current topics.

The split matters: the summary is for reasoning and display; the topics are the **only** recall query.

**2. Recall (coarse ranking).** The topics are embedded into a 1024-d vector (cached in Redis), then used to search Milvus with HNSW/L2. The synchronous fallback asks for `LIMIT_TOP_K` items (18 by default); the asynchronous reranker fetches `SEARCH_TOP_K` candidates (100 by default). Recently recommended items are excluded. If too few candidates remain, the filter is relaxed to avoid starving the feed. Item metadata is loaded cache-first from Redis, then MySQL.

**3. Rerank (LLM decision).** An async worker fetches the same candidate set, numbers the item summaries, and asks the LLM to choose `LIMIT_TOP_K` items (18 by default) plus an `outline`. The prompt asks for diversity, not just maximal similarity. The model can only return indices into the supplied list; the service bounds-checks them.

**4. Delivery: always one window ahead.** `GET /api/items/recommend` never calls an LLM synchronously:

- **Cache hit:** return the previously reranked window (`source=llm`), enqueue the next rerank, increment the cache-hit counter.
- **Cache miss:** run vector recall now (`source=coarse`), return immediately, enqueue a rerank for a later window.

The first request is usually coarse; the next scroll is usually LLM-ranked. If the user scrolls faster than reranking, the same cache may be returned again; the frontend de-duplicates by item ID.

**5. Feedback and profile evolution.** The UI reports `click`, `scroll`, `like`, and `view` events; the backend appends each one to a Redis Stream (max 40 entries, 14-day TTL). Every fifth `source=llm` response, an incremental profile update task reads the new interactions (split from history by a Redis pointer), asks the LLM to update the profile, writes back the summary/topics, invalidates the topic vector, and advances the pointer only after success.

**6. Anti-repeat.** A Redis ZSET tracks recently shown items with a score equal to the number of rerank rounds since they were shown. Each rerank ages all scores by one, removes items past `RECS_WINDOW` rounds, and inserts the new window at score 0. Recall excludes the current window. With `RECS_WINDOW=10` and 18 items per window, up to ~180 items can be excluded — the window is measured in **rounds**, not items.

### Design details worth knowing

| Topic | Behavior |
| --- | --- |
| “Streaming” | An endless feed of recommendation **windows**. Not token-by-token LLM streaming. |
| Latency hiding | Current window from cache/coarse; next window prepared asynchronously. |
| Profile is the query | No user question is required. The profile is editable and evolves from behavior. |
| Recent filter | Best-effort: if recall is too small, the filter is dropped. The client dedupes. |
| Degradation | LLM failure leaves the cached/coarse feed intact; profile updates retry later. |
| Empty profile | `200` + empty list + enqueue profile generation; not a 5xx. |
| Empty catalog | Recall errors as 500. Seed the catalog before opening the feed. |
| Storage split | Milvus stores `(id, vec)`; all metadata is in MySQL/Redis. |

### Tech stack

| Layer | Stack |
| --- | --- |
| Frontend | Vue 3.5, Vite 6, Pinia, Vue Router, Axios, KaTeX |
| Backend | Go 1.26, Gin, GORM, asynq (Redis queue), zerolog |
| Data | MySQL 8 (users/items), Redis 7 (cache/streams/queue), Milvus 2.5 (vectors) |
| AI | DeepSeek chat completions (profile/rerank), DashScope embeddings, Qwen summary/translation |
| Ops | Docker Compose, Vite proxy or Nginx reverse proxy |

Full versions and rationale: [docs/tech-stack.md](docs/tech-stack.md).

---

## 3. Quick start

### 3.1 Try LitStream online

The reference app is running at **[https://nexel.dpdns.org](https://nexel.dpdns.org)**.

1. Register with an email address (the demo uses email verification codes) or log in if you already have an account.
2. Fill in a short profile and pick a few interest areas.
3. Wait for the profile to generate, then scroll the feed.
4. The first window is usually vector recall; later windows are LLM-reranked. Open a card, like it, or scroll a few windows to see the profile evolve.

The demo is a personal-hosted research deployment. If it is unavailable, run the stack locally with the steps below.

### 3.2 Run it with Docker Compose

#### Prerequisites

- Docker Engine/Desktop with Compose v2 (`docker compose version`).
- 4 GB free RAM minimum, 8 GB recommended. Milvus standalone, MySQL, Redis, etcd, and MinIO are the memory-heavy services.
- Node.js 18+ and Yarn 1.x (or npm) for the frontend.
- API keys: DeepSeek (backend LLM), DashScope (embeddings), Alibaba Cloud DirectMail (signup email). Optional: Qwen-MT or Alibaba MT for Chinese translations.

#### Step 1 — Create `.env`

From the repository root:

```bash
cp .env.example .env
# Windows PowerShell: Copy-Item .env.example .env
```

Set at least:

| Variable | What to put there |
| --- | --- |
| `ACCESS_TOKEN_SECRET`, `REFRESH_TOKEN_SECRET` | Long random strings |
| `MODEL_API_KEY`, `MODEL_NAME` | DeepSeek key and a model your key can access |
| `EMBEDDING_MODEL_API_KEY`, `EMBEDDING_MODEL_NAME` | DashScope key and embedding model |
| `EMBEDDING_MODEL_BASE_URL_WITH_OPENAI` | DashScope OpenAI-compatible base URL |
| `ALIYUN_ACCESS_KEY_ID`, `ALIYUN_ACCESS_KEY_SECRET` | DirectMail credentials for signup/reset email |
| `MAIL_ACCOUNT_NAME` | A verified DirectMail sender address |
| `SUMMARY_MODEL_API_KEY` | Key for the Qwen MaaS summary endpoint used by the searcher |

Keep the Compose defaults for `MYSQL_DSN`, `REDIS_ADDRESS`, and `MILVUS_ADDRESS` unless you change the Compose services too.

> The Compose MySQL password is a **development default** (`123456`) and is also present in the sample DSN. Change both before exposing the stack.

Every variable is documented in [docs/configuration.md](docs/configuration.md).

#### Step 2 — Start the backend and data layer

```bash
docker compose up -d --build
docker compose ps
curl http://127.0.0.1:8080/hello
# {"message":"Hello, World!"}
```

The first Milvus start can take 60–90 seconds. The backend waits for MySQL, Redis, and Milvus health.

#### Step 3 — Seed the catalog

**The feed needs items. The command below uses the reference arXiv searcher; if you already have a catalog pipeline, load your own items and skip it — the backend only needs matching MySQL rows and Milvus vectors.**

Ingest one day of arXiv papers (the searcher container is a one-shot `tools` profile job):

```bash
docker compose --profile tools run --rm searcher \
  -config /app/.env \
  -date 2025-01-15 \
  -max 200
```

- `-date` is the arXiv `submittedDate` in **US Eastern Time**.
- Omit `-max` to ingest the whole day.
- Check the result: `docker compose --profile tools run --rm searcher -config /app/.env -op stats`.

An empty Milvus collection surfaces as a 500, not a friendly empty feed, so seed first.

#### Step 4 — Start the frontend

```bash
cd frontend
yarn install
yarn dev
# or: npm install && npm run dev
```

Open **http://127.0.0.1:5173**. The Vite dev server proxies `/api` to `http://127.0.0.1:8080`.

The frontend is not part of `docker-compose.yml`; the Nginx service is commented out and its referenced files are not in the repository. For production, build `frontend/dist` and serve it behind Nginx/Caddy with `/api` proxied to the backend. See [docs/getting-started.md](docs/getting-started.md#5-production-build).

#### Step 5 — First session

1. Register (email code, 5-minute validity, 60-second resend cooldown).
2. Fill the profile and at least one interest.
3. Wait for the profile summary to appear.
4. Scroll to the bottom. The first window is usually `coarse`; the next is usually `llm`.
5. Click, like, and scroll a few windows — this feeds the incremental profile update.

#### Common first-run problems

| Symptom | Fix |
| --- | --- |
| Backend exits with `Can't find the file .env` | Run from a directory containing `.env`, or let Compose mount the root `.env` |
| Milvus connection refused | Wait for health; check `MILVUS_ADDRESS` and `docker compose logs milvus` |
| `embedding 维度不符` | Make `VEC_DIM` match the model; recreate the collection and re-ingest |
| Empty/500 feed | Seed the catalog; run `-op stats` |
| `LLM API 返回 401` | Check `MODEL_API_KEY`; the backend endpoint is DeepSeek-only |
| No signup email | Verify the DirectMail sender/domain and RAM send permission; wait 60 s |
| No Chinese translations | Configure `TRANSLATE_PROVIDER` and its credentials; see [configuration](docs/configuration.md) |

More: [docs/getting-started.md](docs/getting-started.md#7-troubleshooting).

---

## 4. Performance

> **TBD.**
>
> No benchmark numbers are published yet. This section will be filled after the author completes measurements. Planned dimensions: first-window latency, cache-hit latency, rerank completion time, tokens and cost per window, ingestion throughput, single-host memory footprint, and concurrent-user behavior.

---

## 5. Other

### 5.1 Reusing YuStream for another vertical

Start with the [vertical reuse guide](docs/reuse-guide.md). The short version:

1. **Choose any ingestion path.** `searcher/` is optional; keep it if you want the arXiv reference, or write a loader for your API, crawl, CMS, CSV, or queue.
2. Produce the invariant: raw item fields → semantic text → embedding vector, with the same ID written to MySQL and Milvus.
3. Decide the one text field that gets embedded; keep it the same text the reranker sees.
4. Rewrite the prompt set in `backend/llm/prompt.go` and, if you use the reference searcher, the summary prompt in `searcher/llm/summary.go`.
5. Keep the profile/topics split and the output schemas (tool call fields, 1-based indices, outline).
6. Adapt the interaction events and card/detail copy.
7. Tune `SEARCH_TOP_K`, `LIMIT_TOP_K`, `RECS_WINDOW`, and TTLs; evaluate on a fixed candidate set.

If your items do not fit the existing schema, you will also touch the item model, the ingestion mapping, and the frontend display helpers. The guide lists all of them.

### 5.2 Extending or optimizing the framework

YuStream is designed to be changed in two ways:

- **Case extension:** add a new vertical or product scenario on top of the existing loop. See the [vertical reuse guide](docs/reuse-guide.md).
- **Framework optimization:** replace or improve the core — retrieval, reranking, profiling, queueing, caching, cost, observability, or multi-tenancy. See [Extending and optimizing](docs/extending.md).

Both are legitimate. You do not need to fork to add a vertical, and you do not need to preserve every current component to improve the framework. The contracts that should stay stable (item ID mapping, API shape, tool-call schema, Redis state semantics) are listed in the extending guide.

### 5.3 Code structure

- [Backend structure](docs/backend-structure.md) — layers, startup wiring, sync vs async paths, task types, prompts, and known rough edges.
- [Frontend structure](docs/frontend-structure.md) — routes, layouts, stores, feed mechanics, interactions, and build/deploy.
- [Data pipeline](docs/data-pipeline.md) — the reference arXiv pipeline and the ingestion contract; `searcher/` itself is optional.

### 5.4 Documentation

| Document | What it covers |
| --- | --- |
| [docs/README.md](docs/README.md) | Documentation index and reading paths |
| [中文文档](docs/README.zh-CN.md) | Chinese translations of every documentation page |
| [Getting started](docs/getting-started.md) | Local and Docker setup, seeder, production build, troubleshooting |
| [Configuration](docs/configuration.md) | Every `.env` variable, defaults, and change matrix |
| [Architecture](docs/architecture.md) | The loop, data model, failure handling, trade-offs, limitations |
| [Tech stack](docs/tech-stack.md) | Versions, ports, provider choices, and what is intentionally absent |
| [Backend structure](docs/backend-structure.md) | Go package map and extension points |
| [Frontend structure](docs/frontend-structure.md) | Vue app map and feed/auth flows |
| [Data pipeline](docs/data-pipeline.md) | Reference ingestion and the item/semantic/vector contract |
| [HTTP API](docs/api.md) | Endpoints, payloads, status codes, rate limits |
| [Vertical reuse guide](docs/reuse-guide.md) | Adaptation, migration, cost, evaluation, compliance |
| [Extending and optimizing](docs/extending.md) | Case extensions, framework-level changes, contracts, migrations |

### 5.5 Contributing

- Read [Architecture](docs/architecture.md) and the relevant structure doc before changing a layer.
- Keep the layer rules: controllers/workers are thin; business decisions live in `service`; repositories do not call LLMs or enqueue tasks.
- Prompts are part of the product surface. Change them with an evaluation case, not just a vibe check.
- Run `go build ./cmd` for the backend and `yarn build` for the frontend before opening a PR. The `backend/test/` directory contains scratch/integration programs that expect live services; do not treat it as a normal unit-test suite.
- Both kinds of contribution are welcome: a new vertical case (prompts + mapping + sample data contract) and a framework optimization (retrieval, reranker, profile, serving, evaluation). See [Extending and optimizing](docs/extending.md).
- Do not commit `.env`, API keys, or personal data.

### 5.6 Security

The reference deployment binds the backend to `127.0.0.1` and assumes a same-origin reverse proxy. Before public use: change default database credentials, rotate all keys, add TLS, keep MySQL/Redis/Milvus/MinIO private, define a profile/interaction deletion policy, and review your LLM provider's data-retention terms.

---

<div align="center">
<sub>Built around a simple idea: the profile is the query.</sub>
</div>
