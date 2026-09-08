from __future__ import annotations

import argparse
import json
import sys

from .config import BrainConfig
from .context import ContextCompiler
from .docs import DocumentationEngine
from .handoff import HandoffManager
from .indexer import RepositoryIndexer
from .intent import BatchStore, IntentStore
from .ollama import OllamaClient, OllamaError
from .orchestrator import DelegationOrchestrator
from .router import ModelRouter
from .telemetry import AdaptivePolicy, TelemetryStore
from .worker import LocalWorkerPool


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
    policy = AdaptivePolicy(config)
    print(json.dumps({
        "target": decision.target,
        "reason": decision.reason,
        "score": complexity.score,
        "allow_fallback": decision.allow_fallback,
        "flags": sorted(complexity.flags),
        "adaptive_local_multiplier": policy.local_multiplier(),
        "adaptive_local_complexity": policy.effective_local_complexity(),
    }, indent=2))
    return 0


def cmd_delegate(args) -> int:
    plan = DelegationOrchestrator(_config(args)).plan(args.task)
    print(json.dumps(plan.to_dict(), indent=2))
    return 0


def cmd_worker_run(args) -> int:
    config = _config(args)
    pool = LocalWorkerPool(config)
    tasks = [pool.parse_task(value) for value in args.task]
    results = pool.run(tasks, concurrency=args.concurrency)
    print(json.dumps([result.to_dict() for result in results], indent=2, ensure_ascii=False))
    return 0 if all(result.ok for result in results) else 2


def cmd_telemetry(args) -> int:
    config = _config(args)
    store = TelemetryStore(config)
    policy = AdaptivePolicy(config, store)
    payload = {
        "summary": store.summary(args.model),
        "local_model": config.local_model,
        "adaptive_local_multiplier": policy.local_multiplier(),
        "adaptive_local_complexity": policy.effective_local_complexity(),
    }
    if args.tail:
        payload["recent"] = store.records()[-args.tail:]
    print(json.dumps(payload, indent=2, ensure_ascii=False))
    return 0


def cmd_telemetry_mark(args) -> int:
    store = TelemetryStore(_config(args))
    if not store.mark(args.execution_id, args.result == "accepted"):
        print(f"Unknown execution id: {args.execution_id}", file=sys.stderr)
        return 2
    print(json.dumps({"execution_id": args.execution_id, "result": args.result}, indent=2))
    return 0


def cmd_context(args) -> int:
    config = _config(args)
    compiler = ContextCompiler(config)
    if args.explain:
        print(json.dumps(compiler.explain(args.task), indent=2))
    else:
        print(compiler.compile(args.task, max_tokens=args.max_tokens))
    return 0


def cmd_handoff(args) -> int:
    config = _config(args)
    manager = HandoffManager(config)
    path = manager.create(args.task, args.current_tokens, notes=args.note or [])
    print(path)
    return 0


def cmd_handoff_check(args) -> int:
    config = _config(args)
    manager = HandoffManager(config)
    print(json.dumps({
        "should_handoff": manager.should_handoff(args.task, args.current_tokens),
        "current_tokens": args.current_tokens,
        "target_context_tokens": config.target_context_tokens,
        "threshold_ratio": config.handoff_context_threshold,
    }, indent=2))
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

    p = sub.add_parser("delegate", help="estimate whether strong model should delegate locally")
    p.add_argument("task")
    p.set_defaults(func=cmd_delegate)

    p = sub.add_parser("worker-run", help="execute one or more bounded tasks on local Ollama workers")
    p.add_argument("--task", action="append", required=True, help="plain task or JSON {task, task_type, context}")
    p.add_argument("--concurrency", type=int, default=None)
    p.set_defaults(func=cmd_worker_run)

    p = sub.add_parser("telemetry", help="show worker telemetry and learned local routing policy")
    p.add_argument("--model", default=None)
    p.add_argument("--tail", type=int, default=0)
    p.set_defaults(func=cmd_telemetry)

    p = sub.add_parser("telemetry-mark", help="record strong-model acceptance of a worker result")
    p.add_argument("execution_id")
    p.add_argument("result", choices=("accepted", "rejected"))
    p.set_defaults(func=cmd_telemetry_mark)

    p = sub.add_parser("context", help="compile a minimal task context")
    p.add_argument("task")
    p.add_argument("--max-tokens", type=int, default=None)
    p.add_argument("--explain", action="store_true")
    p.set_defaults(func=cmd_context)

    p = sub.add_parser("handoff-check", help="check whether current session should hand off")
    p.add_argument("task")
    p.add_argument("--current-tokens", type=int, required=True)
    p.set_defaults(func=cmd_handoff_check)

    p = sub.add_parser("handoff", help="create a compact fresh-session handoff capsule")
    p.add_argument("task")
    p.add_argument("--current-tokens", type=int, required=True)
    p.add_argument("--note", action="append")
    p.set_defaults(func=cmd_handoff)

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
