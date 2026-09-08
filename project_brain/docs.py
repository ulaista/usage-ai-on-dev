from __future__ import annotations

from pathlib import Path
import json

from .config import BrainConfig


class DocumentationEngine:
    def __init__(self, config: BrainConfig):
        self.config = config

    def bootstrap(self) -> list[Path]:
        self.config.ensure_state()
        created: list[Path] = []
        templates = {
            "PROJECT.md": "# Project\n\n## Purpose\n\nDescribe what the project does.\n\n## Stack\n\nGenerated/maintained by Project Brain.\n",
            "CONVENTIONS.md": "# Conventions\n\nRecord coding, naming, API, testing, and repository conventions here.\n",
            "DEVELOPMENT.md": "# Development\n\n## Setup\n\n## Commands\n\n## Local workflow\n",
            "TESTING.md": "# Testing\n\n## Test strategy\n\n## Commands\n",
            "architecture/OVERVIEW.md": "# Architecture Overview\n\n## Components\n\n## Data flow\n\n## Invariants\n",
            "CHANGELOG.md": "# Project Brain Change Log\n\nSemantic development batches are summarized here.\n",
        }
        for relative, content in templates.items():
            path = self.config.state_dir / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            if not path.exists():
                path.write_text(content, encoding="utf-8")
                created.append(path)
        return created

    def capsule_path(self, source_path: str) -> Path:
        return self.config.state_dir / "capsules" / (source_path.replace("/", "__") + ".md")

    def write_capsule(self, source_path: str, body: str, sha256: str = "") -> Path:
        path = self.capsule_path(source_path)
        path.parent.mkdir(parents=True, exist_ok=True)
        header = f"---\nsource: {source_path}\nsha256: {sha256}\n---\n\n"
        path.write_text(header + body.strip() + "\n", encoding="utf-8")
        return path

    def drift_targets(self, changed_paths: list[str]) -> list[str]:
        targets = {"CHANGELOG.md"}
        lowered = [path.lower() for path in changed_paths]
        if any("api" in path or "controller" in path or "route" in path for path in lowered):
            targets.add("API.md")
        if any("schema" in path or "migration" in path or "model" in path for path in lowered):
            targets.add("DATABASE.md")
        if any("docker" in path or "deploy" in path or "k8s" in path or "terraform" in path for path in lowered):
            targets.add("DEPLOYMENT.md")
        if any("auth" in path or "security" in path or "permission" in path for path in lowered):
            targets.add("SECURITY.md")
        if len({Path(path).parts[0] for path in changed_paths if Path(path).parts}) > 1:
            targets.add("architecture/OVERVIEW.md")
        return sorted(targets)

    def update_manifest(self) -> Path:
        index_path = self.config.state_dir / "state" / "index.json"
        index = json.loads(index_path.read_text(encoding="utf-8")) if index_path.exists() else {"stats": {}}
        manifest = {
            "version": 1,
            "project_root": str(self.config.root),
            "index": index.get("stats", {}),
            "context": {
                "target_tokens": self.config.target_context_tokens,
                "strategy": "hot+selected-warm; raw source on demand",
            },
            "models": {
                "local": self.config.local_model,
                "local_endpoint": self.config.ollama_url,
                "strong": "external coding agent",
            },
        }
        path = self.config.state_dir / "manifest.json"
        path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
        return path
