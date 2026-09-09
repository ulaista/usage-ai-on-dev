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

Serena is an enhancement rather than a hard dependency. If the external semantic process cannot start, the Go core keeps serving SQLite, Git context and repo-map functions while semantic-specific tools report the provider error.

## Ranked repository map

`internal/repomap` provides the first Aider-style repository map layer. It deliberately does not use embeddings or scan the whole repository into the strong model.

The builder:

1. asks Git for tracked code files and blob SHAs;
2. parses compact signatures/imports for supported source types;
3. reuses cached metadata for unchanged Git blobs;
4. resolves local dependency edges where possible;
5. seeds importance from the task, dirty files and Serena evidence;
6. propagates importance over the dependency graph;
7. emits only the highest-ranked entries that fit a strict token budget.

Initial structural extraction is intentionally lightweight:

- Go uses the standard library Go parser/AST;
- Python and JS/TS use bounded import/signature extraction;
- the provider interface leaves room for Tree-sitter or SCIP to replace/augment edge extraction without changing Context Compiler contracts.

The cache lives at `.project-brain/cache/repomap.json`. Files whose Git blob SHA has not changed are reused without reopening/parsing source. Dirty/untracked files are reanalyzed.

Ranking signals currently include:

- current changed file: strongest seed;
- Serena result mentioning a path: strong semantic seed;
- task term in path: medium seed;
- task term in symbol signature: smaller seed;
- dependency graph propagation: related modules inherit part of neighboring importance.

The map is available directly through:

```bash
brain-core --root /project repo-map "refresh token rotation"
```

and MCP tool `brain_repo_map`.

## MCP surface

`brain-core mcp` exposes a stdio MCP server with the V2 tools:

- `brain_status`
- `brain_context`
- `brain_repo_map`
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

The Go Context Compiler now combines:

1. active intent state from SQLite;
2. current Git diff and recent commits;
3. Serena semantic symbols when available;
4. ranked repository-map evidence under a dedicated sub-budget;
5. hard trimming against the overall task context budget.

The repository map receives roughly one fifth of the total context target by default. This keeps structural context useful without letting it crowd out HOT state or exact current diff evidence.

Raw source should be loaded only when the ranked map, semantic evidence or current diffs are insufficient.

## Migration policy

Do not port Python line-by-line. V2 replaces components by responsibility:

| Python MVP | V2 |
| --- | --- |
| JSON/JSONL state | SQLite |
| regex symbol extraction | Serena/LSP + repo-map structural metadata |
| Python daemon/CLI | Go binary |
| raw JSON prompt contracts | BAML typed contracts |
| context keyword ranking | semantic + dependency graph ranking |
| orchestration constants | measured telemetry/adaptive policy |

## Next implementation batches

1. BAML generated Go client and native local-worker execution from the core.
2. SQLite migration/import from Python JSON/JSONL state.
3. Context telemetry: tokens selected, source classes, repo-map cache hit rate and strong-token savings.
4. MCP resources/prompts for project warm-up and handoff.
5. Upgrade structural edges with Tree-sitter and optional SCIP for large/offline repositories.
6. Add ADR/batch/document retrieval into graph/context ranking.
7. Benchmarks against the Python MVP on small, medium and monorepo fixtures.
