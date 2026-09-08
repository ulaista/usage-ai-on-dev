from __future__ import annotations

import argparse
import json
from pathlib import Path
import sys

from .config import BrainConfig
from .context import ContextCompiler
from .docs import DocumentationEngine
from .indexer import RepositoryIndexer
from .intent import BatchStore, IntentStore
from .ollama import OllamaClient, OllamaError
from .router import ModelRouter


def _config(args) -> BrainConfig:
    return BrainConfig.load(getattr(args, "root", "."))


def cmd_init(args) -> int:
    config = _config(args)
    config.ensure_state()
    config.save()
    docs = DocumentationEngine(config)
    created = docs.bootstrap()
    index = RepositoryIndexer(config).scan()
    docs.update_manifest()
    print(f"Project Brain initialized at {config.state_dir}")
    print(f"Indexed {index['stats']['file_count']} text/code files; created {len(created)} docs.")
    return 0


def cmd_index(args) -> int:
    config = _config(args)
    result = RepositoryIndexer(config).scan()
    DocumentationEngine(config).update_manifest()
    print(json.dumps(result["stats"], indent=2))
    return 0


def cmd_route(args) -> int:
    config = _config(args)
    router = ModelRouter(config)
    complexity = router.estimate(args.task)
    decision = router.decide(complexity)
    print(json.dumps({
        "target": decision.target,
        "reason": decision.reason,
        "score": decision.score,
        "allow_fallback": decision.allow_fallback,
        "flags": sorted(complexity.flags),
    }, indent=2))
    return 0


def cmd_context(args) -> int:
    config = _config(args)
    compiler = ContextCompiler(config)
    if args.explain:
        print(json.dumps(compiler.explain(args.task), indent=2))
    else:
        print(compiler.compile(args.task, max_tokens=args.max_tokens))
    return 0


def cmd_intent_create(args) -> int:
    config = _config(args)
    intent = IntentStore(config).create(
        title=args.title,
        goal=args.goal or args.title,
        success=args.success or [],
        constraints=args.constraint or [],
        affected=args.affected or [],
        verification=args.verify or [],
    )
    print(json.dumps(intent.to_dict(), indent=2))
    return 0


def cmd_intent_list(args) -> int:
    intents = IntentStore(_config(args)).list_active()
    print(json.dumps([item.to_dict() for item in intents], indent=2))
    return 0


def cmd_intent_close(args) -> int:
    path = IntentStore(_config(args)).close(args.intent_id)
    print(f"Closed {args.intent_id}: {path}")
    return 0


def cmd_batch_create(args) -> int:
    batch = BatchStore(_config(args)).create(args.intent_id, args.title, args.scope or [])
    print(json.dumps({
        "id": batch.id,
        "intent_id": batch.intent_id,
        "title": batch.title,
        "scope": batch.scope,
        "status": batch.status,
    }, indent=2))
    return 0


def cmd_capsule(args) -> int:
    config = _config(args)
    source = (config.root / args.path).resolve()
    try:
        source.relative_to(config.root)
    except ValueError:
        raise SystemExit("Source must be inside project root")
    content = source.read_text(encoding="utf-8")
    index = RepositoryIndexer(config).scan()
    record = index["files"].get(args.path)
    if not record:
        raise SystemExit(f"File is not indexed as text/code: {args.path}")
    client = OllamaClient(config.ollama_url, config.local_model)
    try:
        summary = client.summarize(args.path, content, num_ctx=args.num_ctx)
    except OllamaError as exc:
        print(str(exc), file=sys.stderr)
        return 2
    path = DocumentationEngine(config).write_capsule(args.path, summary, record["sha256"])
    print(path)
    return 0


def cmd_drift(args) -> int:
    config = _config(args)
    index = RepositoryIndexer(config).scan()
    changed = index["stats"]["changed"] + index["stats"]["deleted"]
    targets = DocumentationEngine(config).drift_targets(changed)
    print(json.dumps({"changed": changed, "documentation_targets": targets}, indent=2))
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="brain", description="Project Brain context orchestrator")
    parser.add_argument("--root", default=".", help="project root")
    sub = parser.add_subparsers(dest="command", required=True)

    p = sub.add_parser("init", help="initialize project memory")
    p.set_defaults(func=cmd_init)

    p = sub.add_parser("index", help="incrementally index repository")
    p.set_defaults(func=cmd_index)

    p = sub.add_parser("route", help="estimate task complexity and model target")
    p.add_argument("task")
    p.set_defaults(func=cmd_route)

    p = sub.add_parser("context", help="compile a minimal task context")
    p.add_argument("task")
    p.add_argument("--max-tokens", type=int, default=None)
    p.add_argument("--explain", action="store_true")
    p.set_defaults(func=cmd_context)

    p = sub.add_parser("intent-create", help="create persistent development intent")
    p.add_argument("title")
    p.add_argument("--goal")
    p.add_argument("--success", action="append")
    p.add_argument("--constraint", action="append")
    p.add_argument("--affected", action="append")
    p.add_argument("--verify", action="append")
    p.set_defaults(func=cmd_intent_create)

    p = sub.add_parser("intent-list", help="list active intents")
    p.set_defaults(func=cmd_intent_list)

    p = sub.add_parser("intent-close", help="archive an intent")
    p.add_argument("intent_id")
    p.set_defaults(func=cmd_intent_close)

    p = sub.add_parser("batch-create", help="create semantic change batch")
    p.add_argument("intent_id")
    p.add_argument("title")
    p.add_argument("--scope", action="append")
    p.set_defaults(func=cmd_batch_create)

    p = sub.add_parser("capsule", help="summarize one source file through local Ollama")
    p.add_argument("path")
    p.add_argument("--num-ctx", type=int, default=32768)
    p.set_defaults(func=cmd_capsule)

    p = sub.add_parser("drift", help="suggest documentation updates from changed files")
    p.set_defaults(func=cmd_drift)

    return parser


def main() -> None:
    parser = build_parser()
    args = parser.parse_args()
    raise SystemExit(args.func(args))


if __name__ == "__main__":
    main()
