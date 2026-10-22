# YuStream documentation

[English](README.md) · [中文](README.zh-CN.md)

YuStream is a prompt-first, vector-native framework for streaming recommendations in vertical products. **LitStream** is the reference application in this repository: an arXiv paper recommender.

The code and some identifiers still use the name `litstream` (module name, Docker project name, database name). Treat `litstream` as the reference implementation, and `YuStream` as the framework it demonstrates.

## What is required, and what is not

- **Required:** the recommendation loop, the user/profile model, the HTTP API, and a catalog that satisfies the item contract.
- **Required data contract:** `raw item information → item semantic text → embedding vector`, written as a MySQL `items` row and a Milvus `(id, vec)` with the same ID.
- **Not required:** `searcher/`, it is a reference producer for arXiv. Your catalog can come from an official API, a crawl, a CMS export, a CSV, a queue, or an internal job. User business, this project has basic user functions. Your main work is to integrate the framework’s recommendation service into your original business.
- **Two ways to build on it:** a new vertical case (domain extension) or a change to the core loop (framework optimization).

## Start here

| Document | What it answers |
| --- | --- |
| [Getting started](getting-started.md) | How do I run LitStream locally or with Docker Compose? |
| [Configuration](configuration.md) | What does every `.env` variable do, and which ones are required? |
| [Architecture](architecture.md) | How does the recommendation loop work, and why is it designed this way? |
| [Tech stack](tech-stack.md) | Which frameworks, services, and versions are used? |
| [Backend structure](backend-structure.md) | Where do I change routing, business logic, storage, prompts, or workers? |
| [Frontend structure](frontend-structure.md) | Where do I change the feed, auth flow, cards, or interactions? |
| [Data pipeline](data-pipeline.md) | What is the ingestion contract, and how does the reference arXiv pipeline work? |
| [HTTP API](api.md) | What are the endpoints, payloads, and response semantics? |
| [Vertical reuse guide](reuse-guide.md) | How do I adapt YuStream to courses, jobs, products, or another vertical? |
| [Extending and optimizing](extending.md) | How do I extend a case or change the framework core safely? |

中文文档：[docs/README.zh-CN.md](README.zh-CN.md).

## Reading paths

**I want to run it**

1. [Getting started](getting-started.md)
2. [Configuration](configuration.md)
3. [Data pipeline](data-pipeline.md) — seed the catalog before opening the feed.

**I want to understand the design**

1. [Architecture](architecture.md)
2. [Tech stack](tech-stack.md)
3. [Backend structure](backend-structure.md) and [frontend structure](frontend-structure.md)

**I want to reuse it for another vertical**

1. [Vertical reuse guide](reuse-guide.md)
2. [Data pipeline](data-pipeline.md) — the ingestion contract; `searcher/` is only a reference
3. [Architecture](architecture.md) — the parts that are framework-level vs domain-level

**I want to change the framework itself**

1. [Extending and optimizing](extending.md) — contracts, migrations, and high-leverage changes
2. [Architecture](architecture.md) — the loop and failure modes
3. [Backend structure](backend-structure.md) — the layer boundaries

**I want to contribute**

1. [Architecture](architecture.md) — read the loop and the failure modes first
2. [Backend structure](backend-structure.md) or [frontend structure](frontend-structure.md)
3. [HTTP API](api.md) — keep the contract stable

## Conventions used in these docs

- **YuStream** — the framework and design pattern.
- **LitStream** — the arXiv application in this repository.
- **Window** — one batch of recommendations returned by `GET /api/items/recommend`. "Streaming" means an endless sequence of windows, not token-by-token streaming.
- **Coarse** — vector recall without an LLM call (`source=coarse`).
- **Reranked** — an LLM-selected window served from the recommendation cache (`source=llm`).
- **Profile** — the natural-language `summary` plus the short-term `interest_topics`. Only `interest_topics` is embedded for recall.
- **Item contract** — the MySQL row + Milvus vector mapping described in [Data pipeline](data-pipeline.md#the-data-contract).

## Current status

YuStream is a research and engineering prototype. The recommendation loop, auth, ingestion, and Docker setup are implemented. Performance and cost benchmarks are **not published yet**; see the placeholder in the [main README](../README.md#4-performance). The code also contains a few intentionally documented rough edges, listed at the end of [Architecture](architecture.md#9-known-limitations) and [Backend structure](backend-structure.md#10-known-rough-edges).

Documentation is maintained in English and Chinese. English pages use `*.md`; Chinese pages use `*.zh-CN.md`. Each page has a language switcher at the top.
