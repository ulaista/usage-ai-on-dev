from __future__ import annotations

from dataclasses import asdict
from datetime import datetime, timezone
from pathlib import Path
import json
import re

from .config import BrainConfig
from .models import ChangeBatch, Intent


def _slug(value: str) -> str:
    value = re.sub(r"[^a-zA-Z0-9]+", "-", value.strip().lower()).strip("-")
    return value[:48] or "task"


class IntentStore:
    def __init__(self, config: BrainConfig):
        self.config = config
        config.ensure_state()

    def create(self, title: str, goal: str, **kwargs) -> Intent:
        stamp = datetime.now(timezone.utc).strftime("%Y%m%d%H%M%S")
        intent = Intent(id=f"INT-{stamp}-{_slug(title)}", title=title, goal=goal, **kwargs)
        path = self.config.state_dir / "intents" / "active" / f"{intent.id}.json"
        intent.save(path)
        return intent

    def list_active(self) -> list[Intent]:
        result: list[Intent] = []
        for path in sorted((self.config.state_dir / "intents" / "active").glob("*.json")):
            result.append(Intent(**json.loads(path.read_text(encoding="utf-8"))))
        return result

    def close(self, intent_id: str) -> Path:
        source = self.config.state_dir / "intents" / "active" / f"{intent_id}.json"
        if not source.exists():
            raise FileNotFoundError(intent_id)
        data = json.loads(source.read_text(encoding="utf-8"))
        data["status"] = "completed"
        destination = self.config.state_dir / "intents" / "completed" / source.name
        destination.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
        source.unlink()
        return destination


class BatchStore:
    def __init__(self, config: BrainConfig):
        self.config = config
        config.ensure_state()

    def create(self, intent_id: str, title: str, scope: list[str] | None = None) -> ChangeBatch:
        stamp = datetime.now(timezone.utc).strftime("%Y%m%d%H%M%S")
        batch = ChangeBatch(
            id=f"BATCH-{stamp}-{_slug(title)}",
            intent_id=intent_id,
            title=title,
            scope=scope or [],
        )
        self.save(batch)
        return batch

    def save(self, batch: ChangeBatch) -> Path:
        path = self.config.state_dir / "batches" / f"{batch.id}.json"
        path.write_text(json.dumps(asdict(batch), indent=2) + "\n", encoding="utf-8")
        return path
