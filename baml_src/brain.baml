client<llm> LocalQwen {
  provider "openai-generic"
  options {
    base_url "http://127.0.0.1:11434/v1"
    model "qwen3.5:4b"
    temperature 0
  }
}

class Evidence {
  source string
  detail string
}

class WorkerEvidence {
  answer string
  evidence Evidence[]
  uncertainty float
  affected_symbols string[]
  suggested_verification string[]
}

class RouteDecision {
  executor "local" | "strong" | "local_then_verify"
  complexity int
  confidence float
  reasons string[]
  required_context string[]
  verification_level "light" | "normal" | "strict"
}

function ClassifyAndRoute(task: string, project_context: string, telemetry_summary: string) -> RouteDecision {
  client LocalQwen
  prompt #"
    You are the low-cost routing analyst for Project Brain.
    Never route architecture, security, destructive migrations, breaking public APIs, or production incidents to local-only execution.

    Task:
    {{ task }}

    Project context:
    {{ project_context }}

    Recent telemetry:
    {{ telemetry_summary }}

    Decide the cheapest safe execution route. Prefer local work only when the result is bounded and cheaply verifiable.

    {{ ctx.output_format }}
  "#
}

function ExecuteBoundedWorker(task: string, context: string) -> WorkerEvidence {
  client LocalQwen
  prompt #"
    You are a bounded local coding worker. Do only the assigned task.
    Do not make architecture, security, destructive migration, breaking API, or production-risk decisions.
    Produce concise evidence so a stronger model can verify without repeating all of your work.

    Task:
    {{ task }}

    Context:
    {{ context }}

    {{ ctx.output_format }}
  "#
}
