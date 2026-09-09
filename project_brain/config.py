from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
import json


@dataclass(slots=True)
class BrainConfig:
    root: Path
    state_dir: Path
    ollama_url: str = "http://127.0.0.1:11434"
    local_model: str = "qwen3.5:4b"
    target_context_tokens: int = 30_000
    max_local_complexity: int = 7
    local_then_escalate_complexity: int = 14
    delegation_overhead_tokens: int = 450
    min_delegation_token_saving: int = 500
    estimated_local_task_seconds: float = 8.0
    estimated_strong_task_seconds: float = 3.0
    handoff_context_threshold: float = 0.72
    local_worker_concurrency: int = 2
    local_worker_timeout_seconds: int = 180
    local_worker_num_ctx: int = 32768
    adaptive_min_samples: int = 20
    max_preferred_local_latency_seconds: float = 20.0
    ignore_dirs: set[str] = field(default_factory=lambda: {
        ".git", ".project-brain", "node_modules", ".venv", "venv", "dist", "build", "coverage", ".next", ".idea",
    })

    @classmethod
    def load(cls, root: str | Path = ".") -> "BrainConfig":
        root_path = Path(root).resolve()
        state_dir = root_path / ".project-brain"
        config_path = state_dir / "config.json"
        config = cls(root=root_path, state_dir=state_dir)
        if config_path.exists():
            data = json.loads(config_path.read_text(encoding="utf-8"))
            for key in (
                "ollama_url", "local_model", "target_context_tokens",
                "max_local_complexity", "local_then_escalate_complexity",
                "delegation_overhead_tokens", "min_delegation_token_saving",
                "estimated_local_task_seconds", "estimated_strong_task_seconds",
                "handoff_context_threshold", "local_worker_concurrency",
                "local_worker_timeout_seconds", "local_worker_num_ctx",
                "adaptive_min_samples", "max_preferred_local_latency_seconds",
            ):
                if key in data:
                    setattr(config, key, data[key])
            if "ignore_dirs" in data:
                config.ignore_dirs = set(data["ignore_dirs"])
        return config

    def ensure_state(self) -> None:
        for relative in (
            "architecture/adr", "intents/active", "intents/completed", "batches",
            "capsules", "summaries", "state", "handoffs", "telemetry",
        ):
            (self.state_dir / relative).mkdir(parents=True, exist_ok=True)

    def save(self) -> None:
        self.ensure_state()
        payload = {
            "ollama_url": self.ollama_url,
            "local_model": self.local_model,
            "target_context_tokens": self.target_context_tokens,
            "max_local_complexity": self.max_local_complexity,
            "local_then_escalate_complexity": self.local_then_escalate_complexity,
            "delegation_overhead_tokens": self.delegation_overhead_tokens,
            "min_delegation_token_saving": self.min_delegation_token_saving,
            "estimated_local_task_seconds": self.estimated_local_task_seconds,
            "estimated_strong_task_seconds": self.estimated_strong_task_seconds,
            "handoff_context_threshold": self.handoff_context_threshold,
            "local_worker_concurrency": self.local_worker_concurrency,
            "local_worker_timeout_seconds": self.local_worker_timeout_seconds,
            "local_worker_num_ctx": self.local_worker_num_ctx,
            "adaptive_min_samples": self.adaptive_min_samples,
            "max_preferred_local_latency_seconds": self.max_preferred_local_latency_seconds,
            "ignore_dirs": sorted(self.ignore_dirs),
        }
        (self.state_dir / "config.json").write_text(
            json.dumps(payload, indent=2) + "\n", encoding="utf-8"
        )
