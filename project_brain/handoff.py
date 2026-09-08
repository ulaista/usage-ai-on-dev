from __future__ import annotations

from datetime import datetime, timezone
import json
from pathlib import Path

from .config import BrainConfig
from .context import ContextCompiler


class HandoffManager:
    def __init__(self, config: BrainConfig):
        self.config = config

    def should_handoff(self, task: str, current_context_tokens: int) -> bool:
        threshold = int(self.config.target_context_tokens / self.config.handoff_context_threshold)
        return current_context_tokens >= threshold

    def create(self, task: str, current_context_tokens: int, notes: list[str] | None = None) -> Path:
        self.config.ensure_state()
        compiler = ContextCompiler(self.config)
        relevant = compiler.relevant_files(task, limit=12)
        active = []
        for path in sorted((self.config.state_dir / "intents" / "active").glob("*.json"))[-3:]:
            try:
                active.append(json.loads(path.read_text(encoding="utf-8")))
            except (OSError, json.JSONDecodeError):
                continue
        batches = []
        for path in sorted((self.config.state_dir / "batches").glob("*.json"))[-5:]:
            try:
                batches.append(json.loads(path.read_text(encoding="utf-8")))
            except (OSError, json.JSONDecodeError):
                continue

        stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
        output = self.config.state_dir / "handoffs" / f"HANDOFF-{stamp}.md"
        body = [
            "# Session Handoff",
            "",
            f"Task: {task}",
            f"Previous context estimate: {current_context_tokens} tokens",
            "",
            "## Active intents",
            "```json",
            json.dumps(active, indent=2),
            "```",
            "",
            "## Recent batches",
            "```json",
            json.dumps(batches, indent=2),
            "```",
            "",
            "## Relevant files",
            *[f"- {item}" for item in relevant],
            "",
            "## Continuation notes",
            *[f"- {item}" for item in (notes or ["Continue from the active intent and verify any uncommitted work before editing."])],
            "",
            "## Session rule",
            "Load this handoff plus only the relevant capsules/raw files. Do not reconstruct the previous chat unless required.",
        ]
        output.write_text("\n".join(body) + "\n", encoding="utf-8")
        return output
