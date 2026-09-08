from __future__ import annotations

from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Any
import json


@dataclass(slots=True)
class FileRecord:
    path: str
    sha256: str
    size: int
    language: str
    summary: str = ""
    symbols: list[str] = field(default_factory=list)
    imports: list[str] = field(default_factory=list)


@dataclass(slots=True)
class Intent:
    id: str
    title: str
    goal: str
    success: list[str] = field(default_factory=list)
    constraints: list[str] = field(default_factory=list)
    affected: list[str] = field(default_factory=list)
    risk: dict[str, str] = field(default_factory=dict)
    verification: list[str] = field(default_factory=list)
    status: str = "active"

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)

    def save(self, path: Path) -> None:
        path.write_text(json.dumps(self.to_dict(), indent=2) + "\n", encoding="utf-8")


@dataclass(slots=True)
class ChangeBatch:
    id: str
    intent_id: str
    title: str
    scope: list[str] = field(default_factory=list)
    files_changed: list[str] = field(default_factory=list)
    decisions: list[str] = field(default_factory=list)
    tests: list[str] = field(default_factory=list)
    unresolved: list[str] = field(default_factory=list)
    next_action: str = ""
    status: str = "planned"


@dataclass(slots=True)
class Complexity:
    scope: int = 0
    ambiguity: int = 0
    architecture: int = 0
    security: int = 0
    cross_module: int = 0
    irreversibility: int = 0
    unknown_code: int = 0
    flags: set[str] = field(default_factory=set)

    @property
    def score(self) -> int:
        return sum((
            self.scope, self.ambiguity, self.architecture, self.security,
            self.cross_module, self.irreversibility, self.unknown_code,
        ))
