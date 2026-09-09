# Claude Code adapter

Read and follow @AGENTS.md. Project Brain is the canonical persistent memory for this repository.

For every user task, call the `project-brain` MCP tools before broad repository exploration:

1. Call `brain_status`.
2. Call `brain_developer_flow` with a unique session ID and the user's task. This performs project recovery, impact analysis, context compilation, and model routing in one pass.
3. Read the returned context and routing decision before opening raw source files.
4. For work spanning multiple files or architectural decisions, call `brain_intent_create`, then split the implementation with `brain_batch_create`.
5. Before completion, run relevant verification and the Project Brain drift workflow required by @AGENTS.md.

Use the local Ollama worker only when Project Brain routes the task to it. Escalate security, database migration, breaking API, production incident, and architecture decisions to the strongest available model.
