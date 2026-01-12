# Chat module

This module owns chat-specific **use-cases**: retrieval, response generation, maintenance, and (optional) graph syncing.

## Entry points
- Public module entry point is `internal/modules/chat` (`Usecases` in `usecases.go`).
- `internal/modules/chat/steps` contains implementation details; pipelines/handlers should call the module, not steps.
