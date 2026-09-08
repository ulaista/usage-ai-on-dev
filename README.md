# Project Brain

Project Brain is a local engineering-intelligence layer for coding agents such as Codex and Claude Code. It keeps durable project memory outside chat sessions, selects compact task context, delegates bounded cheap work to local models, and exposes the result through MCP.

## Current architecture

The project now has two implementations:

- **V2 / production direction:** native Go core under `cmd/` + `internal/`.
- **V1 / reference MVP:** Python package under `project_brain/` used for behavior comparison and migration tests.

Do not extend the Python indexer as the long-term semantic engine. V2 uses a provider boundary and delegates code intelligence to Serena/LSP by default.

```text
Codex / Claude / strong model
           |
           | MCP
           v
     Project Brain Core
            Go
           |
   +-------+---------+----------------+
   |                 |                |
 SQLite          Serena/LSP       BAML contracts
 intents         symbols/refs      local Qwen
 batches
 telemetry
```

See [`docs/ARCHITECTURE_V2.md`](docs/ARCHITECTURE_V2.md) for the migration plan and component boundaries.

## V2 capabilities already implemented

- Native Go core and CLI (`brain-core`).
- SQLite machine state in `.project-brain/brain.db` using WAL mode.
- Durable intents and semantic change batches.
- Execution telemetry schema for model latency, tokens, fallbacks and strong-model acceptance.
- Semantic provider interface.
- Serena MCP adapter for `find_symbol`, `find_referencing_symbols`, and symbol overview operations.
- Project Brain MCP server using the official Go MCP SDK.
- Typed BAML contracts for routing and bounded local-worker evidence.
- Python reference MVP retained for A/B comparison.

## Build V2

Requires Go 1.25+.

```bash
go build -o brain-core ./cmd/brain-core
```

Initialize a project:

```bash
./brain-core --root /path/to/project init
```

Inspect core state:

```bash
./brain-core --root /path/to/project status
./brain-core --root /path/to/project telemetry
```

Test semantic lookup through Serena:

```bash
./brain-core --root /path/to/project semantic-find AuthService
```

By default Serena is launched through `uvx` using the command stored in `.project-brain/config.v2.json`. The semantic backend is replaceable; a future SCIP provider can implement the same interface.

## Run as MCP server

```bash
./brain-core --root /path/to/project mcp
```

Initial V2 MCP tools:

- `brain_status`
- `brain_intent_create`
- `brain_intent_list`
- `brain_batch_create`
- `brain_telemetry`
- `brain_find_symbol`
- `brain_find_references`

The coding agent should talk to this stable surface instead of depending directly on Serena, SQLite or the local-model implementation.

## Local AI contracts

`baml_src/brain.baml` defines typed contracts for:

- `ClassifyAndRoute`
- `ExecuteBoundedWorker`
- `RouteDecision`
- `WorkerEvidence`

The initial local client targets Ollama's OpenAI-compatible endpoint:

```text
http://127.0.0.1:11434/v1
qwen3.5:4b
```

BAML is the AI contract layer, not the systems runtime. Storage, MCP lifecycle, Git and process supervision stay in Go.

## Strong/local execution philosophy

The strong model remains planner, architecture owner, risk owner and final verifier.

```text
strong model
   |
   +--> architecture/security/risky changes ----------> strong
   |
   +--> bounded summaries/docs/extraction --> local Qwen
                                                |
                                         compact evidence
                                                |
strong model <----------------------------------+
   |
selective verification
```

A local worker should return evidence, uncertainty and affected symbols. The strong model should spot-check the relevant sources/diff/tests instead of repeating the entire local analysis.

## Context policy

Project Brain should compile context from:

1. active intent and current batch;
2. current Git diff and relevant history;
3. semantic symbols/references from Serena;
4. an Aider-style repository map ranked under a strict token budget;
5. relevant ADRs, batches and documentation capsules;
6. compact local-worker evidence.

Raw source should be loaded only when these layers are insufficient.

The next implementation batch is the Go **Git-aware Context Compiler + repo-map ranking**. That will replace the Python MVP's keyword-based context selection.

## Python reference MVP

The Python implementation remains usable while V2 reaches parity:

```bash
pip install -e '.[dev]'
brain init
brain index
brain context "task" --explain
brain delegate "task"
brain worker-run --task "summarize changed files"
brain telemetry
```

It includes incremental hashing, intents/batches, handoff capsules, local worker pooling and adaptive telemetry. It is now a reference implementation, not the target runtime architecture.

## CI

Pull requests run two independent jobs:

```text
Go V2
  gofmt
  go test ./...
  go build ./cmd/brain-core

Python reference
  pytest
```

This lets us keep behavior coverage while migrating responsibilities instead of doing a risky line-by-line rewrite.

## Roadmap

1. Git-aware Context Compiler in Go.
2. Aider-style repository map and graph ranking under token budget.
3. Generated BAML Go client and local-worker execution inside the core.
4. Import/migration of Python JSON/JSONL state into SQLite.
5. Context telemetry: cache hits, selected sources and strong-model token savings.
6. MCP resources/prompts for session warm-up and compact handoff.
7. Optional SCIP semantic provider for very large/offline repositories.
8. Benchmarks: Python MVP vs Go V2 on small, medium and monorepo fixtures.
9. Only move proven CPU hot paths to Rust/Mojo if profiling justifies it.
