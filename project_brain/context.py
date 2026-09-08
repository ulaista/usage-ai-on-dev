from __future__ import annotations

from pathlib import Path
import json

from .config import BrainConfig


class ContextCompiler:
    def __init__(self, config: BrainConfig):
        self.config = config

    def _index(self) -> dict:
        path = self.config.state_dir / "state" / "index.json"
        if not path.exists():
            return {"files": {}, "stats": {}}
        return json.loads(path.read_text(encoding="utf-8"))

    @staticmethod
    def _tokens(text: str) -> int:
        # Intentionally conservative and dependency-free; providers can replace this estimator.
        return max(1, len(text) // 4)

    def relevant_files(self, task: str, limit: int = 20) -> list[str]:
        terms = {term.lower() for term in task.replace("/", " ").replace("_", " ").split() if len(term) > 2}
        ranked: list[tuple[int, str]] = []
        for path, record in self._index().get("files", {}).items():
            haystack = " ".join([
                path,
                record.get("language", ""),
                " ".join(record.get("symbols", [])),
                " ".join(record.get("imports", [])),
            ]).lower()
            score = sum(3 if term in path.lower() else 1 for term in terms if term in haystack)
            if score:
                ranked.append((score, path))
        ranked.sort(key=lambda item: (-item[0], item[1]))
        return [path for _, path in ranked[:limit]]

    def compile(self, task: str, max_tokens: int | None = None) -> str:
        max_tokens = max_tokens or self.config.target_context_tokens
        sections: list[str] = []
        budget = 0

        project_doc = self.config.state_dir / "PROJECT.md"
        if project_doc.exists():
            text = project_doc.read_text(encoding="utf-8")
            sections.append("# PROJECT\n" + text)
            budget += self._tokens(text)

        architecture = self.config.state_dir / "architecture" / "OVERVIEW.md"
        if architecture.exists():
            text = architecture.read_text(encoding="utf-8")
            if budget + self._tokens(text) <= max_tokens:
                sections.append("# ARCHITECTURE\n" + text)
                budget += self._tokens(text)

        active_dir = self.config.state_dir / "intents" / "active"
        for intent_path in sorted(active_dir.glob("*.json"))[-3:]:
            text = intent_path.read_text(encoding="utf-8")
            if budget + self._tokens(text) <= max_tokens:
                sections.append("# ACTIVE INTENT\n```json\n" + text + "```\n")
                budget += self._tokens(text)

        candidates = self.relevant_files(task)
        for relative in candidates:
            capsule = self.config.state_dir / "capsules" / (relative.replace("/", "__") + ".md")
            if capsule.exists():
                text = capsule.read_text(encoding="utf-8")
                estimated = self._tokens(text)
                if budget + estimated <= max_tokens:
                    sections.append(f"# CAPSULE: {relative}\n{text}")
                    budget += estimated

        sections.append(
            "# CONTEXT SELECTION\n"
            f"Task: {task}\n"
            f"Estimated tokens: {budget}\n"
            f"Relevant source candidates: {', '.join(candidates) if candidates else 'none indexed'}\n"
            "Load raw source only when the capsule or current diff is insufficient.\n"
        )
        return "\n\n".join(sections)

    def explain(self, task: str) -> dict:
        context = self.compile(task)
        return {
            "estimated_tokens": self._tokens(context),
            "relevant_files": self.relevant_files(task),
            "target_context_tokens": self.config.target_context_tokens,
        }
