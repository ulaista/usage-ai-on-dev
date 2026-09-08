# Project Brain V2 architecture

V2 turns Project Brain from an agent framework into a local engineering-intelligence service used by Codex, Claude Code, and other MCP-capable coding agents.

## Runtime split

```text
Codex / Claude / other strong model
              |
              | MCP
              v
       Project Brain Core
              Go
              |
    +---------+----------+----------------+
    |                    |                |
    v                    v                v
 SQLite state       SemanticProvider   AI contracts
                        |                BAML
                        v                  |
                    Serena/LSP             v
                                      Ollama/Qwen
```

The strong model remains planner, architecture owner and final verifier. Project Brain supplies durable state, compact context, measured routing data and low-cost local workers.

## Why Go

The core is dominated by filesystem, Git, SQLite, HTTP/MCP, process supervision and concurrent worker orchestration. Go provides a native single-binary runtime and inexpensive concurrency without forcing the whole system into a systems-programming complexity budget.

Python remains in `project_brain/` as the reference MVP until V2 reaches feature parity and benchmark coverage.

## Machine state

SQLite at `.project-brain/brain.db` is the machine source of truth. Current tables:

- `intents`
- `batches`
- `executions`

The database runs in WAL mode with foreign keys enabled. Markdown under `.project-brain/` remains the human/LLM-readable projection layer, not the primary transactional store.

## Semantic intelligence

The core defines a `SemanticProvider` interface. The default provider is Serena, launched as an external MCP process and activated against the current project root.

Project Brain currently maps these semantic operations:

- symbol lookup -> Serena `find_symbol`
- reference lookup -> Serena `find_referencing_symbols`
- file overview -> Serena `get_symbols_overview`

This deliberately replaces the prototype regex symbol extractor for V2. Future providers can implement the same interface with SCIP or another code-intelligence backend.

## MCP surface

`brain-core mcp` exposes a stdio MCP server with the first V2 tools:

- `brain_status`
- `brain_intent_create`
- `brain_intent_list`
- `brain_batch_create`
- `brain_telemetry`
- `brain_find_symbol`
- `brain_find_references`

The goal is a small stable contract. Coding agents should not know whether a symbol came from Serena, SCIP, or another backend.

## Typed local AI

`baml_src/brain.baml` defines typed contracts for:

- `ClassifyAndRoute`
- `ExecuteBoundedWorker`
- `RouteDecision`
- `WorkerEvidence`

The initial BAML client targets Ollama's OpenAI-compatible endpoint with `qwen3.5:4b`. This layer is intentionally outside the Go core build until generated-client integration is added. It lets prompts, structured outputs and evals evolve without coupling storage or MCP lifecycle code to an LLM framework.

## Context strategy

The next Context Compiler should combine:

1. active intent and batch state from SQLite;
2. current Git diff and recent relevant commits;
3. Serena semantic symbols/references;
4. an Aider-style repository map with graph ranking under a token budget;
5. relevant ADR/batch/document capsules;
6. local-worker evidence packets.

Raw source should be loaded only when semantic summaries or current diffs are insufficient.

## Migration policy

Do not port Python line-by-line. V2 replaces components by responsibility:

| Python MVP | V2 |
| --- | --- |
| JSON/JSONL state | SQLite |
| regex symbol extraction | Serena/LSP |
| Python daemon/CLI | Go binary |
| raw JSON prompt contracts | BAML typed contracts |
| context keyword ranking | semantic + graph ranking |
| orchestration constants | measured telemetry/adaptive policy |

## Next implementation batches

1. Git-aware Context Compiler in Go.
2. Repo-map graph ranking with a strict token budget.
3. BAML generated Go client and local-worker execution from the core.
4. SQLite migration/import from Python JSON/JSONL state.
5. Context telemetry: tokens selected, source classes and cache hit rate.
6. MCP resources/prompts for project warm-up and handoff.
7. Optional SCIP provider for large/offline repositories.
8. Benchmarks against the Python MVP on small, medium and monorepo fixtures.
