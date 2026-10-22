# Vertical reuse guide

[English](reuse-guide.md) · [中文](reuse-guide.zh-CN.md)

This guide is for adapting YuStream/LitStream to another vertical: courses, jobs, products, events, news, media, internal documents, or any catalog where each item has a text description.

The honest version of the reuse story:

- If your items can be represented by the existing fields (`title`, `abstract`/description, `authors`/brand, `fields`/tags, `summary`), adaptation is mostly **prompts + ingestion + copy**.
- If your items need different fields, you also change the **item schema** in the backend, your ingestion pipeline, and the frontend.
- You do **not** need to train a ranking model or build a feature pipeline.
- You do **not** need to use `searcher/`. It is an arXiv reference producer; your ingestion path can be anything that produces the item contract.

## 1. What is framework-level and what is domain-level

| Layer | Framework-level | Domain-level (change for a new vertical) |
| --- | --- | --- |
| Recommendation loop | Cache/coarse/rerank/feedback | Which interactions matter, update cadence |
| Retrieval | Embedding + Milvus + top-K + recent filter | Embedding model, item text, top-K |
| Rerank | Forced tool call, 1-based indices, outline | Prompt, diversity objective, item count |
| Profile | `summary` + `interest_topics` split | Profile dimensions, information-need model |
| Catalog | MySQL `items` + Milvus `(id, vec)` | Fields, source, dedupe key, enrichment prompt |
| Ingestion | The `raw → semantic text → vector → (MySQL + Milvus, same id)` contract | Source, tooling, mapping, enrichment |
| UI | Feed, cards, detail, onboarding | Copy, card layout, interaction events |

Keep the **profile → topics → embedding → recall → rerank → interactions → profile** loop. Change the content and the judgment criteria inside it.

### Case extension vs framework optimization

This guide is about **case extension**: shipping a new vertical on the existing loop. If you want to change the loop itself — retrieval, reranker, profile engine, serving, cost, or multi-tenancy — read [Extending and optimizing](extending.md). You can do both, but keep the contracts in that document stable unless you are deliberately migrating them.

## 2. Seven-step adaptation

### Step 1 — Define the item and the embedded text

Write down, in one paragraph, what a user is choosing between and what text best represents an item. Then choose **one** embedding text field.

Examples:

| Vertical | Item | Embedded text (example) |
| --- | --- | --- |
| Papers | arXiv paper | Problem + method + result summary |
| Courses | Course | Level + syllabus + skills + format |
| Jobs | Job post | Role + responsibilities + stack + seniority |
| Products | SKU | Category + use case + materials + differentiators |
| Events | Event | Topic + audience + format + speakers |
| Internal docs | Document | Purpose + decisions + systems affected |

The same text should be shown to the reranker (or a concise derivative of it). In LitStream, `items.summary` is both the embedded document and the reranker input.

### Step 2 — Build the ingestion adapter (or reuse the reference)

`searcher/` is **optional**. It is a complete reference for arXiv; use it as a template, or write a loader in any language. What is worth borrowing is the **flow**, not necessarily the implementation: raw item information → semantic text → vector → matching writes.

The invariant is:

```text
raw item information → item semantic text → embedding vector
                                   │
                    MySQL `items` row + Milvus `(id, vec)` with the same id
```

Rules:

1. **Pick your source and tooling.** Official API, crawl, CMS export, CSV, queue, or an internal job are all fine.
2. **Map fields** into the `items` columns the backend reads. Keep a stable dedupe key. LitStream uses `items.url`; for products use a SKU, for jobs a requisition ID, for courses a course code. If `url` is not stable, add a dedicated identifier column and update the dedupe query.
3. **Generate the semantic text** and store it in `items.summary` (or your chosen field). If you use an LLM for this, keep the output as **plain text**, because it is embedded directly.
4. **Write MySQL first**, get the item ID, then write `(id, vec)` to Milvus. Do not let the two IDs diverge.
5. **Dedupe before enrichment** so you do not pay for LLM/translation calls on items already stored.
6. **Verify** with a small batch before a full import: row/vector counts, a sample vector search, and a sample `GET /api/items/recommend`.

If you keep the reference searcher, replace `searcher/arxiv/` with your source client and rewrite `ingest/mapping.go`; the rest of its flow still applies.

### Step 3 — Choose the embedding model and dimension

- Use the same model in the backend (for `interest_topics`) and in your ingestion pipeline (for item text). The reference searcher reads the same `.env` for this reason.
- Set `VEC_DIM` to the model output. The sample uses 1024.
- Use a separate Milvus collection per vertical. Do not mix vectors from different models.
- If you change the model later, create a new collection, re-embed everything, then switch `MILVUS_COLLECTION_NAME`.

### Step 4 — Rewrite the prompts

The prompt set lives in [`backend/llm/prompt.go`](../backend/llm/prompt.go), with one more in [`searcher/llm/summary.go`](../searcher/llm/summary.go):

| Prompt | Keep invariant | Rewrite for the domain |
| --- | --- | --- |
| `PromptGenerateUS` | Output `summary` + `interest_topics`; topics are the recall query | What a good profile means; which needs are predictable |
| `PromptUpdateUS` | Use new/history interactions; keep topics format stable | Which behaviors signal interest; how fast interests decay |
| `PromptUpdateWithNewInfo` | Minimal edits unless the field change is large | Which fields are meaningful |
| `PromptRecommend` | Choose only supplied indices; return `outline`; allow diversity | What "good" means; diversity dimensions; risk/coverage |
| Reference searcher summary prompt (optional) | Plain text, no hallucination, 300–500 tokens | Problem/method/result becomes role/skills/fit, etc. |

#### Profile prompt checklist

- Define the profile as **long-term + inferred information needs**, not a list of keywords.
- Define `interest_topics` as **short-term, concrete, and changeable**. It is embedded; one paragraph of prose is a bad recall query.
- Tell the model to keep the topic format stable across updates (e.g., always "topic: subtopic" lines) so vectors remain comparable.
- Include a "certain vs uncertain needs" split if that helps the reranker.
- Specify the JSON/tool schema and forbid extra text.

#### Rerank prompt checklist

- Give the model a clear objective: satisfy the inferred information need, not maximize similarity.
- Require diversity (methods, sources, price bands, seniority, topics).
- Forbid external knowledge and invented items.
- Explain the 1-based index contract and the exact output field names.
- Keep the candidate format terse: `index. summary`.
- Decide whether to show the model the profile summary, the topics, or both. LitStream uses both; topics anchor recall, the summary supplies intent.

#### A reusable rerank template

```text
You are a recommendation decision engine for <vertical>.
Given a user profile, current interest topics, and a numbered candidate list,
select exactly <N> items that best satisfy the user's information need.
Prefer diversity across <dimensions>; do not select near-duplicates.
Use only the supplied candidates. Do not invent facts.
Return JSON with:
- items_list: 1-based candidate indices, length <N>
- outline: one paragraph describing this window
```

### Step 5 — Adapt interactions and UI

| LitStream event | Generic meaning | Vertical example |
| --- | --- | --- |
| `click` | Open detail / outbound | Open job, add to cart, view syllabus |
| `scroll` | Dwell on a window | Same |
| `like` | Save/favorite | Shortlist, bookmark |
| `view` | Detail dwell | Read time, video watch time |

Change:

- `backend/domain/models_domain.go` (`InteractionType`) if you add a type.
- `backend/llm/prompt.go` to explain what each type means.
- `frontend/src/api/interactions.js` to emit it.
- `frontend/src/components/*` to capture it (buttons, timers, scroll depth).

If a signal is noisy (e.g., accidental clicks), weight it in the prompt or aggregate it before sending. The backend stores raw events; the prompt is where interpretation lives.

### Step 6 — Tune retrieval and rerank

| Knob | Effect | Starting point |
| --- | --- | --- |
| `SEARCH_TOP_K` | Recall breadth and prompt tokens | 50–100 |
| `LIMIT_TOP_K` | Window size and user choice load | 10–20 |
| `RECS_WINDOW` | Anti-repeat rounds | 5–10 |
| `RECOMMEND_TTL` | How long a reranked window is reusable | 15–30 min |
| Update cadence | Profile freshness vs LLM cost | Every 3–5 LLM windows |
| Summary length | Embedding quality vs prompt cost | 200–500 tokens |

For small catalogs (<10k items), you can lower `SEARCH_TOP_K`; for broad catalogs, raise it, but watch prompt size. If the reranker frequently returns fewer than `LIMIT_TOP_K` items, check the prompt's index contract and candidate count.

### Step 7 — Evaluate before claiming quality

At minimum:

- **Offline:** build 30–100 labeled (profile/query → relevant items) cases. Measure recall@K of the coarse stage and precision/NDCG of the reranked window. Compare prompt versions on the same candidate sets.
- **Online:** click-through, like rate, scroll depth/dwell, window-to-window retention, diversity, and "not interested" if you add it.
- **Cost:** tokens per window, LLM calls per session, embedding calls per profile update.
- **Failure:** empty windows, malformed tool calls, out-of-range indices, profile update failures.

Do not compare a prompt change against a different candidate set; hold recall fixed.

## 3. Version migration

### Prompt-only change

Current state: prompts are Go constants. The workflow is:

1. Edit `backend/llm/prompt.go`.
2. Rebuild/restart the backend (`docker compose up -d --build backend` or `go run ./cmd`).
3. Re-run a fixed offline evaluation set.
4. Canary one instance or one user cohort if you have routing.

There is no prompt registry. If you need A/B tests, add a `PROMPT_VERSION` env and select a prompt set at startup, or externalize prompts into a table/file.

### Embedding-model or dimension change

This is a data migration, not a config tweak:

1. Create a new collection with the new `VEC_DIM`.
2. Re-embed every item (`regen-embedding` after a full re-ingest, or a dedicated migration job).
3. Invalidate all `vec:user:*` caches.
4. Switch `MILVUS_COLLECTION_NAME` / `VEC_DIM`.
5. Keep the old collection until the new one passes evaluation; then delete it.

Never write old-model and new-model vectors into one collection.

### Item schema change

Change all of:

- `backend/domain/models_domain.go` (GORM + JSON fields)
- `searcher/domain/item.go` (must stay column-compatible)
- `searcher/ingest/mapping.go`
- `frontend/src/utils/format.js` and the card/detail components
- prompts that refer to fields

Then run a MySQL migration (AutoMigrate adds columns, but it does not transform data) and backfill.

### LLM provider change

The backend endpoint is hardcoded to DeepSeek; the searcher summary endpoint is hardcoded to Qwen MaaS. To use another provider:

1. Add a base URL + auth header config to `backend/pkg/llm.go` (and `searcher/llm/client.go`).
2. Verify the provider supports forced tool calls or switch to strict JSON output.
3. Re-test parsing, truncation, and out-of-range indices.
4. Re-run the offline evaluation; model changes alter ranking behavior.

## 4. Multi-vertical or multi-tenant

The current deployment is single-vertical:

- one `MILVUS_COLLECTION_NAME`
- one prompt set
- one `items` table

For multiple verticals, the minimal design is:

| Concern | Change |
| --- | --- |
| Vectors | Collection per vertical (`<vertical>_items`) |
| Prompts | Prompt set selected by vertical/tenant |
| Items | `vertical_id` column + scoped queries |
| Profiles | A user's profile per vertical, or a global profile plus vertical interests |
| Routing | Resolve vertical from host/path/tenant, then select collection + prompts |
| Caching | Include vertical in every Redis key |

This is not implemented. Do not assume tenant isolation from the current keys.

## 5. Cost control

The loop's cost is dominated by:

1. **Rerank calls:** one per LLM response. Reduce `SEARCH_TOP_K`, shorten summaries, increase cache TTL, or skip reranking for low-traffic users.
2. **Profile updates:** one every 5 LLM responses. Increase the interval or debounce interactions.
3. **Ingestion:** one summary + one translation per new item. Dedupe before enrichment is already implemented; do not backfill blindly.
4. **Embeddings:** one per item, one per profile change. Cache vectors (already done) and avoid unnecessary profile rewrites.

Cheap levers first: shorter summaries, lower top-K, fewer windows per session, and batch ingestion off-peak.

## 6. Legal and compliance

- Respect the source's terms, robots policy, and rate limits. arXiv has API usage expectations; commercial catalogs may require a license.
- Store only what you need. Profiles and interaction streams can contain personal data.
- Tell users that an LLM processes their profile and interactions, and link a privacy policy.
- Provide deletion/export paths. MySQL rows are durable; Redis TTLs are not a deletion policy.
- Do not use the system for regulated decisions (credit, employment, housing, medical) without domain review and human oversight.
- Check the LLM/embedding provider's data-retention and cross-border transfer terms.

## 7. Adaptation checklist

- [ ] Ingestion path chosen; `searcher/` is optional, and the item contract is clear.
- [ ] Item schema and embedded text are defined.
- [ ] Source adapter + dedupe key are implemented.
- [ ] The ingestion pipeline and backend use the same embedding model/dimension.
- [ ] A small batch is ingested and verified (row/vector counts, sample search, sample feed).
- [ ] Profile prompts define long-term needs + short-term topics.
- [ ] Rerank prompt enforces indices, diversity, and the outline.
- [ ] Interaction meanings are documented and emitted by the UI.
- [ ] `SEARCH_TOP_K` / `LIMIT_TOP_K` / `RECS_WINDOW` / TTLs are tuned.
- [ ] Offline evaluation cases exist and prompt changes are compared on fixed candidates.
- [ ] Failure paths are tested: LLM timeout, empty collection, Redis loss, profile missing.
- [ ] Privacy/legal review is done.

## 8. Anti-patterns

- **Treating `searcher/` as mandatory.** It is one reference producer; any pipeline that writes the item contract works.
- **Embedding a long profile summary.** Recall becomes noisy; embed the short topics instead.
- **Letting the LLM invent item IDs.** Always map indices back to supplied candidates and bounds-check.
- **Skipping coarse fallback.** Waiting for the LLM on every request destroys the feed experience.
- **Using one collection for multiple embedding models.** The distances are meaningless.
- **Treating `RECS_WINDOW` as item count.** It is rounds; multiply by `LIMIT_TOP_K` to estimate excluded items.
- **Updating the profile after every event.** Cost and churn; batch or debounce.
- **Optimizing only for similarity.** The prompt should ask for diversity and information-need fit.
- **Claiming quality without evaluation.** The loop is plausible; it is not proven until measured.

For changes to the loop itself rather than a new vertical, see [Extending and optimizing](extending.md).
