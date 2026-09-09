# Project Brain

Project Brain is a local engineering-intelligence layer for coding agents such as Codex and Claude Code. It keeps durable project memory outside chat sessions, selects compact task context, delegates bounded cheap work to local models, and exposes the result through MCP.

## Current architecture

The project has two implementations:

- **V2 / production direction:** native Go core under `cmd/` + `internal/`.
- **V1 / reference MVP:** Python package under `project_brain/` used for behavior comparison and migration tests.

Do not extend the Python indexer as the long-term semantic engine. V2 uses Serena/LSP for precise semantic intelligence and a local dependency graph for cheap repository-wide structure.

```text
Codex / Claude / strong model
           |
           | MCP
           v
     Project Brain Core
            Go
           |
   +--------+----------+-------------+
   |                   |             |
 SQLite            Serena/LSP     BAML
 state              symbols/refs   local Qwen
   |
   +------> Git + ranked Repo Map
                dependency graph
                token budget
```

See `docs/ARCHITECTURE_V2.md` for component boundaries and migration policy.

## V2 capabilities implemented

- Native Go core and CLI (`brain-core`).
- SQLite machine state in `.project-brain/brain.db` using WAL mode.
- Durable intents and semantic change batches.
- Execution telemetry schema for model latency, tokens, fallbacks and strong-model acceptance.
- Semantic provider interface with Serena MCP adapter.
- Graceful operation when Serena is unavailable.
- Git-aware Context Compiler using current diff, recent commits and active intents.
- Aider-inspired ranked repository map with dependency-graph propagation.
- Repo-map cache keyed by Git blob SHA.
- Go AST structural extraction plus bounded Python/JS/TS extraction.
- Strict token budgets for repo map, semantic evidence and final context.
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

Compile task-specific context:

```bash
./brain-core --root /path/to/project context "implement refresh token rotation"
```

Inspect only the ranked repository map:

```bash
./brain-core --root /path/to/project repo-map "implement refresh token rotation"
```

Test semantic lookup through Serena:

```bash
./brain-core --root /path/to/project semantic-find AuthService
```

By default Serena is launched through `uvx` using the command stored in `.project-brain/config.v2.json`. The semantic backend is replaceable; SCIP can implement the same interface later.

## Ranked repository map

The repo map is generated locally without asking an LLM to read the repository.

```text
Git tracked files + dirty files
             |
             v
 compact structural analysis
             |
             v
       dependency graph
             |
   +---------+---------+
   |         |         |
 task      Git diff   Serena
 terms      seeds     evidence
   |         |         |
   +---------+---------+
             |
             v
      graph propagation
             |
             v
      token-budget filter
             |
             v
      compact repo map
```

Ranking favors current changed files, semantic hits and task-relevant symbols, then spreads part of that importance to related modules. For example, changing `auth/service.go` can pull `token/store.go` into context because of the dependency edge even when the task never names that file.

The cache is stored at:

```text
.project-brain/cache/repomap.json
```

Unchanged tracked files reuse their Git blob metadata. Dirty and untracked source files are reanalyzed.

## Run as MCP server

```bash
./brain-core --root /path/to/project mcp
```

V2 MCP tools:

- `brain_status`
- `brain_context`
- `brain_repo_map`
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

BAML is the AI contract layer, not the systems runtime. Storage, MCP lifecycle, Git, repo-map and process supervision stay in Go.

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

A local worker should return evidence, uncertainty and affected symbols. The strong model should spot-check relevant sources, diff and tests instead of repeating the entire local analysis.

## Context policy

Project Brain now compiles context from:

1. active intent state;
2. current Git diff and recent commits;
3. semantic symbols from Serena when available;
4. ranked repository structure under a strict token sub-budget;
5. eventually relevant ADRs, batches and documentation capsules;
6. eventually compact local-worker evidence packets.

Raw source should be loaded only when these layers are insufficient.

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

It includes incremental hashing, intents/batches, handoff capsules, local worker pooling and adaptive telemetry. It is a reference implementation, not the target runtime architecture.

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

## Roadmap

1. Generated BAML Go client and native local-worker execution inside the core.
2. Python JSON/JSONL -> SQLite migration/import.
3. Context/cache/token-savings telemetry.
4. MCP warm-up/handoff resources and prompts.
5. Tree-sitter structural provider and optional SCIP provider.
6. ADR/batch/document retrieval in graph ranking.
7. Python MVP vs Go V2 benchmarks.
