from __future__ import annotations

from dataclasses import asdict
from hashlib import sha256
from pathlib import Path
import json
import re

from .config import BrainConfig
from .models import FileRecord


TEXT_EXTENSIONS = {
    ".py", ".js", ".jsx", ".ts", ".tsx", ".java", ".kt", ".go", ".rs",
    ".c", ".h", ".cpp", ".hpp", ".cs", ".rb", ".php", ".swift", ".vue",
    ".svelte", ".html", ".css", ".scss", ".sql", ".graphql", ".md", ".json",
    ".yaml", ".yml", ".toml", ".ini", ".sh", ".zsh", ".fish", ".dockerfile",
}

LANGUAGE_BY_SUFFIX = {
    ".py": "python", ".js": "javascript", ".jsx": "javascript", ".ts": "typescript",
    ".tsx": "typescript", ".java": "java", ".kt": "kotlin", ".go": "go", ".rs": "rust",
    ".cs": "csharp", ".rb": "ruby", ".php": "php", ".swift": "swift", ".sql": "sql",
    ".md": "markdown", ".json": "json", ".yaml": "yaml", ".yml": "yaml", ".toml": "toml",
}

SYMBOL_PATTERNS = [
    re.compile(r"^\s*(?:async\s+)?def\s+([A-Za-z_][\w]*)", re.M),
    re.compile(r"^\s*class\s+([A-Za-z_][\w]*)", re.M),
    re.compile(r"^\s*(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_$][\w$]*)", re.M),
    re.compile(r"^\s*(?:export\s+)?(?:class|interface|type|enum)\s+([A-Za-z_$][\w$]*)", re.M),
]

IMPORT_PATTERNS = [
    re.compile(r"(?:from|import)\s+['\"]([^'\"]+)['\"]"),
    re.compile(r"^\s*from\s+([\w.]+)\s+import", re.M),
    re.compile(r"^\s*import\s+([\w.]+)", re.M),
]


class RepositoryIndexer:
    def __init__(self, config: BrainConfig):
        self.config = config
        self.index_path = config.state_dir / "state" / "index.json"

    def _is_ignored(self, path: Path) -> bool:
        relative = path.relative_to(self.config.root)
        return any(part in self.config.ignore_dirs for part in relative.parts)

    def _is_text_candidate(self, path: Path) -> bool:
        if path.name in {"Dockerfile", "Makefile", "Procfile"}:
            return True
        return path.suffix.lower() in TEXT_EXTENSIONS

    def _read_existing(self) -> dict[str, dict]:
        if not self.index_path.exists():
            return {}
        data = json.loads(self.index_path.read_text(encoding="utf-8"))
        return data.get("files", {})

    def scan(self) -> dict:
        self.config.ensure_state()
        existing = self._read_existing()
        records: dict[str, dict] = {}
        changed: list[str] = []
        unchanged: list[str] = []

        for path in sorted(self.config.root.rglob("*")):
            if not path.is_file() or self._is_ignored(path) or not self._is_text_candidate(path):
                continue
            try:
                raw = path.read_bytes()
                text = raw.decode("utf-8")
            except (UnicodeDecodeError, OSError):
                continue

            relative = path.relative_to(self.config.root).as_posix()
            digest = sha256(raw).hexdigest()
            old = existing.get(relative)
            if old and old.get("sha256") == digest:
                records[relative] = old
                unchanged.append(relative)
                continue

            symbols: list[str] = []
            imports: list[str] = []
            for pattern in SYMBOL_PATTERNS:
                symbols.extend(pattern.findall(text))
            for pattern in IMPORT_PATTERNS:
                imports.extend(pattern.findall(text))

            record = FileRecord(
                path=relative,
                sha256=digest,
                size=len(raw),
                language=LANGUAGE_BY_SUFFIX.get(path.suffix.lower(), path.suffix.lower().lstrip(".") or "text"),
                symbols=sorted(set(symbols))[:200],
                imports=sorted(set(imports))[:200],
            )
            records[relative] = asdict(record)
            changed.append(relative)

        deleted = sorted(set(existing) - set(records))
        languages: dict[str, int] = {}
        for record in records.values():
            language = record["language"]
            languages[language] = languages.get(language, 0) + 1

        payload = {
            "version": 1,
            "files": records,
            "stats": {
                "file_count": len(records),
                "changed": changed,
                "unchanged_count": len(unchanged),
                "deleted": deleted,
                "languages": dict(sorted(languages.items(), key=lambda item: (-item[1], item[0]))),
            },
        }
        self.index_path.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
        return payload
