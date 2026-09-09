# Project Brain instructions for coding agents

Project Brain is the canonical project-memory layer for this repository.

## Session start

1. For every user task, call the `project-brain` MCP tools before broad repository exploration.
2. Call `brain_status`, then call `brain_developer_flow` with a unique session ID and the user's task.
3. Read the returned context and routing decision before opening raw source files.
4. Let the MCP launcher initialize `.project-brain` automatically when project state is missing; do not require manual CLI setup from the user.
5. If Project Brain routes a bounded low-risk task to the local worker, call `brain_local_run` and verify its evidence before applying changes.
6. Create an intent for work that spans multiple files, sessions, or architectural decisions, then split implementation into semantic change batches.

## Context policy

- Prefer `.project-brain/PROJECT.md`, relevant capsules, active intents, ADRs, and recent relevant batches over replaying chat history.
- Do not load the entire repository merely because the model has a large context window.
- Load raw source only when summaries/capsules are insufficient for the current task.
- Let Project Brain refresh context incrementally; unchanged SHA-256 records should not be re-summarized.

## Model routing

- Low-risk classification, summarization, documentation, diff analysis, and straightforward maintenance may use the local Ollama worker.
- Security, database migrations, breaking public APIs, production incidents, and architecture decisions require the strong coding model.
- Medium-complexity local work must allow escalation when verification fails or uncertainty remains.

## Completion

Before closing an intent:

1. Run relevant tests and verification.
2. Run `brain drift` and update affected documentation.
3. Record architectural decisions as ADRs when they create cross-cutting or durable constraints.
4. Persist the batch summary and unresolved work.
5. Close the intent only when its success criteria are met.
