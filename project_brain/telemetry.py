from __future__ import annotations

from dataclasses import dataclass, asdict
from datetime import datetime, timezone
from pathlib import Path
import json
import statistics
import uuid

from .config import BrainConfig


@dataclass(slots=True)
class ExecutionRecord:
    execution_id: str
    task: str
    task_type: str
    model: str
    route: str
    latency_seconds: float
    input_tokens: int = 0
    output_tokens: int = 0
    accepted: bool | None = None
    fallback: bool = False
    error: str | None = None
    created_at: str = ""

    def to_dict(self) -> dict:
        return asdict(self)


class TelemetryStore:
    def __init__(self, config: BrainConfig):
        self.config = config
        self.config.ensure_state()
        self.path = self.config.state_dir / "telemetry" / "executions.jsonl"
        self.path.parent.mkdir(parents=True, exist_ok=True)

    @staticmethod
    def new_id() -> str:
        return "EXE-" + uuid.uuid4().hex[:12]

    def append(self, record: ExecutionRecord) -> None:
        if not record.created_at:
            record.created_at = datetime.now(timezone.utc).isoformat()
        with self.path.open("a", encoding="utf-8") as fh:
            fh.write(json.dumps(record.to_dict(), ensure_ascii=False) + "\n")

    def records(self) -> list[dict]:
        if not self.path.exists():
            return []
        result = []
        for line in self.path.read_text(encoding="utf-8").splitlines():
            try:
                result.append(json.loads(line))
            except json.JSONDecodeError:
                continue
        return result

    def mark(self, execution_id: str, accepted: bool) -> bool:
        rows = self.records()
        changed = False
        for row in rows:
            if row.get("execution_id") == execution_id:
                row["accepted"] = accepted
                changed = True
        if changed:
            self.path.write_text(
                "".join(json.dumps(row, ensure_ascii=False) + "\n" for row in rows),
                encoding="utf-8",
            )
        return changed

    def summary(self, model: str | None = None) -> dict:
        rows = [r for r in self.records() if model is None or r.get("model") == model]
        if not rows:
            return {"samples": 0}
        latencies = [float(r.get("latency_seconds", 0)) for r in rows if r.get("latency_seconds") is not None]
        reviewed = [r for r in rows if r.get("accepted") is not None]
        accepted = [r for r in reviewed if r.get("accepted") is True]
        return {
            "samples": len(rows),
            "reviewed": len(reviewed),
            "acceptance_rate": round(len(accepted) / len(reviewed), 3) if reviewed else None,
            "avg_latency_seconds": round(statistics.fmean(latencies), 3) if latencies else None,
            "p50_latency_seconds": round(statistics.median(latencies), 3) if latencies else None,
            "input_tokens": sum(int(r.get("input_tokens", 0)) for r in rows),
            "output_tokens": sum(int(r.get("output_tokens", 0)) for r in rows),
            "fallbacks": sum(1 for r in rows if r.get("fallback")),
            "errors": sum(1 for r in rows if r.get("error")),
        }


class AdaptivePolicy:
    def __init__(self, config: BrainConfig, telemetry: TelemetryStore | None = None):
        self.config = config
        self.telemetry = telemetry or TelemetryStore(config)

    def local_multiplier(self) -> float:
        stats = self.telemetry.summary(self.config.local_model)
        if stats.get("samples", 0) < self.config.adaptive_min_samples:
            return 1.0
        acceptance = stats.get("acceptance_rate")
        latency = stats.get("avg_latency_seconds")
        multiplier = 1.0
        if acceptance is not None:
            if acceptance >= 0.9:
                multiplier *= 1.2
            elif acceptance < 0.7:
                multiplier *= 0.65
        if latency is not None and latency > self.config.max_preferred_local_latency_seconds:
            multiplier *= 0.8
        return round(max(0.4, min(1.4, multiplier)), 2)

    def effective_local_complexity(self) -> int:
        return max(1, round(self.config.max_local_complexity * self.local_multiplier()))
