from __future__ import annotations

from dataclasses import dataclass, field

from .config import BrainConfig
from .router import ModelRouter
from .telemetry import AdaptivePolicy, TelemetryStore


LOCAL_TASK_HINTS = {
    "summarize", "summary", "docs", "documentation", "format", "rename",
    "classify", "extract", "index", "changelog", "lint", "simple test",
    "опис", "документ", "формат", "переимен", "классифиц", "индекс",
}

STRONG_TASK_HINTS = {
    "architecture", "security", "migration", "production", "incident",
    "breaking", "redesign", "race condition", "distributed", "архитект",
    "безопас", "миграц", "прод", "инцидент",
}

NON_DELEGABLE_FLAGS = {
    "database_migration", "breaking_api", "production_incident", "architecture_decision",
}


@dataclass(slots=True)
class DelegationPlan:
    task: str
    controller: str
    worker: str | None
    action: str
    estimated_local_seconds: float
    estimated_strong_seconds: float
    estimated_strong_token_saving: int
    verification: str
    reasons: list[str] = field(default_factory=list)
    adaptive_multiplier: float = 1.0

    @property
    def estimated_latency_delta_seconds(self) -> float:
        return round(self.estimated_local_seconds - self.estimated_strong_seconds, 2)

    def to_dict(self) -> dict:
        return {
            "task": self.task,
            "controller": self.controller,
            "worker": self.worker,
            "action": self.action,
            "estimated_local_seconds": self.estimated_local_seconds,
            "estimated_strong_seconds": self.estimated_strong_seconds,
            "estimated_latency_delta_seconds": self.estimated_latency_delta_seconds,
            "estimated_strong_token_saving": self.estimated_strong_token_saving,
            "adaptive_multiplier": self.adaptive_multiplier,
            "verification": self.verification,
            "reasons": self.reasons,
        }


class DelegationOrchestrator:
    """Strong-model controller with telemetry-aware local delegation."""

    def __init__(self, config: BrainConfig):
        self.config = config
        self.router = ModelRouter(config)
        self.telemetry = TelemetryStore(config)
        self.policy = AdaptivePolicy(config, self.telemetry)

    @staticmethod
    def _estimate_tokens(task: str) -> int:
        words = max(1, len(task.split()))
        return max(800, words * 120)

    def plan(self, task: str) -> DelegationPlan:
        text = task.lower()
        complexity = self.router.estimate(task)
        route = self.router.decide(complexity)
        strong_tokens = self._estimate_tokens(task)
        local_hint = any(hint in text for hint in LOCAL_TASK_HINTS)
        strong_hint = any(hint in text for hint in STRONG_TASK_HINTS)
        multiplier = self.policy.local_multiplier()
        effective_limit = self.policy.effective_local_complexity()

        orchestration_overhead = self.config.delegation_overhead_tokens
        saving = max(0, strong_tokens - orchestration_overhead)

        read_only_security = local_hint and complexity.flags == {"security"}
        hard_risk = bool(complexity.flags & NON_DELEGABLE_FLAGS)
        strong_operation = strong_hint and not local_hint

        if hard_risk or strong_operation or (route.target == "strong" and not read_only_security):
            return DelegationPlan(
                task=task,
                controller="strong",
                worker=None,
                action="execute_strong",
                estimated_local_seconds=0,
                estimated_strong_seconds=self.config.estimated_strong_task_seconds,
                estimated_strong_token_saving=0,
                adaptive_multiplier=multiplier,
                verification="strong model owns implementation and verification",
                reasons=[route.reason, f"adaptive local complexity limit={effective_limit}"],
            )

        if complexity.score > effective_limit and not read_only_security and not route.allow_fallback:
            return DelegationPlan(
                task=task,
                controller="strong",
                worker=None,
                action="execute_strong",
                estimated_local_seconds=0,
                estimated_strong_seconds=self.config.estimated_strong_task_seconds,
                estimated_strong_token_saving=0,
                adaptive_multiplier=multiplier,
                verification="strong model owns implementation and verification",
                reasons=[f"complexity {complexity.score} exceeds adaptive local limit {effective_limit}"],
            )

        minimum_saving = round(self.config.min_delegation_token_saving / multiplier)
        if saving < minimum_saving:
            return DelegationPlan(
                task=task,
                controller="strong",
                worker=None,
                action="execute_strong",
                estimated_local_seconds=0,
                estimated_strong_seconds=self.config.estimated_strong_task_seconds,
                estimated_strong_token_saving=0,
                adaptive_multiplier=multiplier,
                verification="normal strong-model verification",
                reasons=[f"expected saving {saving} < adaptive minimum {minimum_saving}"],
            )

        stats = self.telemetry.summary(self.config.local_model)
        local_seconds = stats.get("avg_latency_seconds") or self.config.estimated_local_task_seconds
        if not local_hint and route.allow_fallback:
            local_seconds *= 1.35

        reasons = [route.reason, f"adaptive local complexity limit={effective_limit}", "bounded task can be checked cheaply"]
        if read_only_security:
            reasons.append("security topic is read-only summarization; decisions remain strong-model owned")

        return DelegationPlan(
            task=task,
            controller="strong",
            worker=self.config.local_model,
            action="delegate_local_then_verify",
            estimated_local_seconds=round(local_seconds, 2),
            estimated_strong_seconds=self.config.estimated_strong_task_seconds,
            estimated_strong_token_saving=saving,
            adaptive_multiplier=multiplier,
            verification="strong model validates compact result, diff, tests or structured evidence",
            reasons=reasons,
        )

    def split(self, task: str) -> list[dict]:
        plan = self.plan(task)
        if plan.action == "execute_strong":
            return [{"role": "strong", "task": task, "verify": True}]
        return [
            {
                "role": "local",
                "task": task,
                "output_contract": "return concise result + evidence + uncertainty",
                "verify": False,
            },
            {
                "role": "strong",
                "task": "verify local worker output against project constraints and current diff",
                "verify": True,
            },
        ]
