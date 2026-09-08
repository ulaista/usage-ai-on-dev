from __future__ import annotations

from dataclasses import dataclass

from .config import BrainConfig
from .models import Complexity


STRONG_MODEL_FLAGS = {
    "security", "database_migration", "breaking_api", "production_incident", "architecture_decision",
}


@dataclass(slots=True)
class RouteDecision:
    target: str
    reason: str
    score: int
    allow_fallback: bool = False


class ModelRouter:
    def __init__(self, config: BrainConfig):
        self.config = config

    def decide(self, complexity: Complexity) -> RouteDecision:
        critical = complexity.flags & STRONG_MODEL_FLAGS
        if critical:
            return RouteDecision(
                target="strong",
                reason=f"critical flags: {', '.join(sorted(critical))}",
                score=complexity.score,
            )
        if complexity.score <= self.config.max_local_complexity:
            return RouteDecision(
                target="local",
                reason="low-complexity task suitable for local worker",
                score=complexity.score,
            )
        if complexity.score <= self.config.local_then_escalate_complexity:
            return RouteDecision(
                target="local",
                reason="medium complexity: local attempt with strong-model fallback",
                score=complexity.score,
                allow_fallback=True,
            )
        return RouteDecision(
            target="strong",
            reason="complexity score exceeds local threshold",
            score=complexity.score,
        )

    @staticmethod
    def estimate(task: str) -> Complexity:
        text = task.lower()
        c = Complexity()
        c.scope = 3 if any(x in text for x in ("entire", "whole", "all modules", "monorepo", "весь проект")) else 1
        c.ambiguity = 2 if len(task.split()) < 5 else 1
        c.architecture = 4 if any(x in text for x in ("architecture", "архитект", "redesign", "migration strategy")) else 0
        c.security = 4 if any(x in text for x in ("security", "auth", "oauth", "jwt", "permission", "безопас")) else 0
        c.cross_module = 3 if any(x in text for x in ("cross-module", "several modules", "несколько модул")) else 0
        c.irreversibility = 3 if any(x in text for x in ("delete data", "drop table", "production", "prod")) else 0
        c.unknown_code = 1

        if c.security:
            c.flags.add("security")
        if "migration" in text and any(x in text for x in ("database", "db", "schema", "баз")):
            c.flags.add("database_migration")
        if any(x in text for x in ("breaking api", "breaking change")):
            c.flags.add("breaking_api")
        if any(x in text for x in ("incident", "outage", "production incident")):
            c.flags.add("production_incident")
        if c.architecture >= 4:
            c.flags.add("architecture_decision")
        return c
