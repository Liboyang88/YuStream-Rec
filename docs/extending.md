# Extending and optimizing YuStream

[English](extending.md) · [中文](extending.zh-CN.md)

YuStream is a starting point, not a fixed prescription. There are two legitimate ways to build on it:

1. **Case extension** — instantiate the framework for a new vertical or a new product scenario.
2. **Framework optimization** — improve the core loop itself: retrieval, reranking, profiling, serving, cost, evaluation, or operations.

Both are first-class. You do not need to fork the project to add a vertical, and you do not need to keep every current component to improve the framework.

## 1. Two tracks

| | Case extension | Framework optimization |
| --- | --- | --- |
| Goal | Ship a new vertical feed | Make the framework faster, cheaper, better, or more general |
| Typical owner | Product / application developer | Framework maintainer / researcher |
| Starting point | [Vertical reuse guide](reuse-guide.md) | This document + [Architecture](architecture.md) |
| What changes | Data source, item mapping, prompts, UI copy, interaction set | Retrieval, reranker, profile engine, queue, cache, storage, observability |
| What should stay stable | The item contract, API contract, tool-call contract | The same contracts, unless you deliberately version a migration |
| Fork required? | Usually no | Often yes for deep changes; smaller changes can be upstreamed |

## 2. Case extension: add a vertical

A case extension uses the existing runtime and changes the domain layer:

- **Catalog and ingestion** — bring items from your own source. `searcher/` is only a reference producer; it is not required.
- **Item schema** — map your fields into the existing `items` shape, or add fields in the backend/searcher/frontend.
- **Semantic text** — choose the one text that is embedded and shown to the reranker.
- **Prompts** — rewrite profile generation, profile update, field-change update, and rerank prompts.
- **Interaction model** — decide which behaviors matter and how they are recorded.
- **UI** — cards, detail panel, filters, copy, and the interaction affordances.
- **Evaluation** — define relevance for your domain; do not reuse paper-specific judgments.

The detailed recipe is in the [vertical reuse guide](reuse-guide.md).

### The one invariant that matters

Whatever ingestion path you choose, the runtime expects this chain:

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

The backend does not import the searcher. It only reads MySQL and Milvus. If your pipeline produces this contract, it is compatible with YuStream. Borrow the flow, not necessarily the searcher code.

## 3. Framework optimization: improve the core

The loop has several replaceable parts. Keep the contracts in §4, and you can swap the implementation behind any of them.

| Optimization target | Entry point | Example change |
| --- | --- | --- |
| Ingestion | `searcher/` or your own writer | Add a connector, stream ingestion, incremental updates, dedupe by SKU |
| Embedding | `embedutil`, `searcher/embedder` | New model, dimension, batch strategy, local embedding service |
| Recall | `ItemRepository.SearchVec`, `MilvusDB` | Hybrid BM25 + vector, metadata filters, multiple collections, ANN tuning |
| Rerank | `LLMInvoking.Recommend`, `BuildToolRecommend` | New prompt, listwise vs pairwise, diversity/MMR, deterministic fallback |
| Profile | `PromptGenerateUS`, `PromptUpdateUS`, `UserService` | Structured profile, decay, explicit user controls, multi-profile |
| Feedback | `InteractionRepository`, `PromptUpdateUS` | Event weighting, debounce, sessionization, negative feedback |
| Delivery | `RecommendCache`, `ItemService.GetRecommend` | Per-window keys, prefetch, warmup, streaming transport |
| Queue / workers | `queue/`, `worker/`, `bootstrap` | Separate worker process, priority queues, provider-specific concurrency |
| Storage | `db/`, repositories | Read replicas, Redis eviction policy, collection-per-vertical |
| Cost | prompts, `SEARCH_TOP_K`, TTLs | Candidate pruning, shorter summaries, adaptive update cadence |
| Observability | `pkg/mylog`, middleware | Metrics, traces, token accounting, per-stage latency |
| Safety | prompt/tool layer, API | Moderation, allowlists, PII redaction, audit logs |

### Common optimization directions

**Cost.** The largest recurring cost is the rerank prompt (candidate count × summary length), followed by profile updates and offline enrichment. Reduce candidates, shorten summaries, increase cache reuse, or update the profile less often.

**Latency.** The synchronous path already avoids LLM waits. The next wins are embedding-cache hits, faster item loads, a separate worker process, and warming the next window before the user reaches the bottom.

**Quality.** Add hybrid lexical/vector recall, diversify the reranked set, weight interactions by confidence, and evaluate prompt variants on fixed candidate sets. Do not compare variants on different recalls.

**Scale.** Move the worker out of the HTTP process, shard Milvus by vertical, add MySQL read replicas, and partition Redis by key family. Keep the item ID mapping intact.

**Generality.** Externalize prompts, add provider adapters, support multiple verticals with a `vertical_id` and collection routing, and make the cache key include the vertical.

**Operations.** Add `/metrics`, structured token accounting, per-stage timings, and alerts for empty windows, LLM error rates, queue depth, and Milvus load state.

## 4. Contracts to preserve

Unless you are intentionally versioning a migration, keep these stable:

### Item contract

- MySQL `items.id` is the join key.
- Milvus `(id, vec)` uses the **same** `id`.
- The semantic text used for embedding is stored on the item (in LitStream, `items.summary`).
- Display metadata lives in MySQL; Milvus does not need it.
- Do not mix vectors from different embedding models/dimensions in one collection.

### API contract

- `GET /api/items/recommend` returns `{ items, source, outline }`.
- `source` is `llm`, `coarse`, or `empty`.
- Auth remains JWT-based unless you version the API.
- The frontend assumes no cursor; every request is a new window.

### LLM contract

- Rerank tool call returns `items_list` (1-based indices) and `outline`.
- Profile tool call returns `summary` and `interest_topics`.
- The service bounds-checks indices and maps them back to supplied candidates.
- The profile topics are the only recall query; keep them short and stable in format.

### State contract

- `recommend:current` is one cache slot per user (unless you change the key model).
- `recentlyrecommended` scores mean "rounds since shown", not item count.
- The interaction stream pointer advances only after a successful profile write.
- Redis keys include the user ID; add tenant/vertical to keys if you add multi-tenancy.

### Layer contract

- Controllers and workers are thin adapters.
- Business decisions live in `service`.
- Repositories do not call LLMs or enqueue tasks.
- Prompts live in the LLM package, not in controllers.

See [Backend structure](backend-structure.md) for the full layer map.

## 5. Branching, versioning, and migrations

- **Fork when** you change storage schemas, provider protocols, or serving topology. Smaller changes (a prompt, a connector, a metric) can be upstreamed.
- **Version prompts** with a config flag or a prompt registry if you A/B test them. Do not silently change prompts in production.
- **Migrate embeddings** by creating a new collection, re-embedding everything, invalidating user vectors, and switching traffic only after evaluation.
- **Migrate item schemas** with explicit backfills; AutoMigrate adds columns but does not transform data.
- **Keep a rollback path** for every migration (old collection, old prompt version, old worker image).

## 6. Evaluation guardrails

Any framework optimization should answer:

- Did recall improve on a fixed evaluation set?
- Did the reranked window improve precision/NDCG or online engagement?
- What happened to tokens, cost, and latency?
- Did the empty-window rate or malformed-tool-call rate change?
- Does the change hold across at least two verticals or user cohorts?

Collect offline and online signals; a prompt or model change that looks better in one example may be worse in aggregate.

## 7. Contribution paths

Useful contributions include:

- A new vertical example (with prompts, mapping, and sample data contract).
- A provider adapter (OpenAI-compatible base URL, auth, tool-call differences).
- A better reranker prompt with a small evaluation set.
- A retrieval improvement (hybrid search, filters, ANN tuning).
- Observability, tests, and failure-mode fixes.
- Documentation, diagrams, and translations.

For a code change, keep the layer rules and include the evaluation you used. For a new vertical, include the prompt set and the item mapping so others can reproduce it.

## 8. Cautions

- Do not treat `searcher/` as mandatory. It is one reference producer for arXiv.
- Do not let the LLM invent item IDs or bypass bounds checks.
- Do not write two embedding models into one collection.
- Do not change `RECS_WINDOW` semantics without updating the docs and the ZSET migration.
- Do not add a feature that only works for one vertical into the framework core without a configuration boundary.
- Do not claim an improvement without a fixed evaluation set and a cost check.

YuStream is meant to be forked, extended, and improved. The loop is the stable idea; the components around it are yours to change.
