# Chat module

This module owns chat-specific **use-cases**: retrieval, response generation, thread maintenance (summaries/memory), and optional graph syncing/indexing for better recall and provenance.

## Entry points
- Public entrypoint: `neurobridge-backend/internal/modules/chat/usecases.go` (`chat.Usecases`).
- Implementation details: `neurobridge-backend/internal/modules/chat/steps` (avoid calling from handlers; prefer `Usecases`).
- Search/index helpers: `neurobridge-backend/internal/modules/chat/index`.

## How it’s used

### HTTP (sync)
Chat CRUD endpoints are served by:
- Router: `neurobridge-backend/internal/http/router.go` (`/api/chat/...`)
- Handler: `neurobridge-backend/internal/http/handlers/chat.go`
- Service: `neurobridge-backend/internal/services/chat.go`

### Jobs (async)
Most “expensive” chat work runs as jobs:
- `chat_respond`: retrieval + model response, persists a turn, emits SSE progress
- `chat_maintain`: periodic maintenance (summaries, memory, entity/claim extraction, optional Neo4j sync)
- `chat_path_index`: projects path/learning artifacts into chat retrieval docs (so chats can cite your curriculum)
- `chat_rebuild`: rebuilds derived artifacts (e.g., Pinecone docs) from Postgres
- `chat_purge`: removes derived artifacts

Job handlers live in `neurobridge-backend/internal/jobs/pipeline/*` and call into this module’s use-cases.

## Storage model (Postgres)
Core persisted records live in `neurobridge-backend/internal/domain` with repos under `neurobridge-backend/internal/data/repos/chat`:
- Threads + messages: `ChatThread`, `ChatMessage`
- Derived “docs” used for retrieval: `ChatDoc` (backed by Pinecone when configured)
- Turns: `ChatTurn` (a model response + trace metadata)
- Maintenance artifacts: `ChatSummaryNode`, `ChatMemoryItem`, `ChatEntity`, `ChatClaim`, `ChatEdge`, `ChatThreadState`

## Retrieval model (hybrid + grounded)
Retrieval is intentionally hybrid:
- Dense vector retrieval (Pinecone when available; SQL embedding fallback when needed)
- Lexical retrieval over stored docs
- LLM reranking and selection

Entry point:
- `hybridRetrieve(...)` in `neurobridge-backend/internal/modules/chat/steps/retrieval.go`

Source material grounding for answers:
- `neurobridge-backend/internal/modules/chat/steps/material_chunks_retrieval.go`

## Graph / Neo4j integration (optional)
If `NEO4J_URI` is configured, maintenance steps can upsert a queryable chat graph:
- Neo4j upserts: `neurobridge-backend/internal/data/graph/neo4j_chat_graph.go`
- Maintain step: `neurobridge-backend/internal/modules/chat/steps/maintain_neo4j.go`

The system must remain correct without Neo4j; graph writes are best-effort.

## Extending the chat module (safely)
- Prefer adding new derived artifacts as `ChatDoc` types (so they can be indexed and retrieved).
- Keep retrieval resilient when Pinecone is disabled (fallback paths must still work).
- If adding a new graph projection, treat it as best-effort and never make it the only source of truth.
- Add a job-level regression test when introducing new deterministic behavior (e.g., indexing rules, schema enforcement).
