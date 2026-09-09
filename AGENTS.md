# Project Brain instructions for coding agents

Project Brain is the canonical project-memory layer for this repository.

## Session start

1. Run `brain index` after pulling changes.
2. For a user task, run `brain route "<task>"` and `brain context "<task>" --explain`.
3. Read the compiled context before broad repository exploration.
4. Create an intent for work that spans multiple files, sessions, or architectural decisions.
5. Split implementation into semantic change batches.

## Context policy

- Prefer `.project-brain/PROJECT.md`, relevant capsules, active intents, ADRs, and recent relevant batches over replaying chat history.
- Do not load the entire repository merely because the model has a large context window.
- Load raw source only when summaries/capsules are insufficient for the current task.
- Re-index incrementally; unchanged SHA-256 records should not be re-summarized.

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
