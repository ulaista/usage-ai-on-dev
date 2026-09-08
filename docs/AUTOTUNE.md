# Capability Benchmark and Model Autotuner

Project Brain uses measured local performance to refine the hardware-derived local-AI policy. The autotuner is advisory until the user explicitly accepts its recommendation.

## Lifecycle

```text
hardware detection
      |
      v
safe baseline policy
      |
      v
installed-model discovery (/api/tags)
      |
      v
bounded benchmark trials (/api/chat)
      |
      +--> prompt throughput
      +--> generation throughput
      +--> load / total latency
      +--> available-memory delta
      +--> swap growth
      +--> memory-pressure peak
      |
      v
ranked recommendation
      |
      v
show user / coding agent
      |
 accept / override / ignore
      |
      v
hardware-policy.json
```

The benchmark never downloads models. Only models already installed in the configured Ollama instance are considered.

## CLI

Run the benchmark against up to four installed models:

```bash
brain-core --root /project autotune
```

Benchmark only selected installed models:

```bash
brain-core --root /project autotune "qwen3.5:4b,qwen3.5:8b"
```

Show the last saved report:

```bash
brain-core --root /project autotune-show
```

Accept the measured recommendation:

```bash
brain-core --root /project autotune-accept
```

Accept it but change some recommended limits:

```bash
brain-core --root /project autotune-accept \
  '{"soft_context_tokens":6000,"max_parallel_workers":1}'
```

## MCP tools

- `brain_autotune`
- `brain_autotune_show`
- `brain_autotune_accept`

`brain_autotune` saves a report but returns `applied=false`. A strong coding agent must not treat the recommendation as active until the user accepts it.

## Persistence

Latest benchmark report:

```text
.project-brain/autotune-report.json
```

Accepted effective policy:

```text
.project-brain/hardware-policy.json
```

The report contains the hardware ID used during measurement. `autotune-accept` rejects a report generated on different hardware.

## Safety

A trial is rejected from the safe set when it reaches critical memory pressure or causes excessive swap growth. Failed models remain in the report with their error instead of aborting the whole benchmark.

The runtime resource guard remains authoritative after autotuning. A user-accepted model/context recommendation does not force inference when current memory pressure, swap usage, or context requirements are unsafe.

## Scoring

The initial score favors:

1. measured generation throughput;
2. measured prompt/prefill throughput;
3. larger context sizes that completed safely;
4. penalties for very slow generation.

The scorer is intentionally simple and observable. Future telemetry can include task success rate, strong-model acceptance rate, energy/thermal behavior, model load frequency, and workload-specific profiles.

## Current scope

The first autotuner targets Ollama because Project Brain's bounded local worker currently uses Ollama. The benchmark contract is provider-neutral enough to add MLX, llama.cpp, vLLM, LM Studio, or remote worker providers later.
