from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass, asdict
import json
import time

from .config import BrainConfig
from .ollama import OllamaClient, OllamaError
from .telemetry import ExecutionRecord, TelemetryStore


SYSTEM = (
    "You are a bounded Project Brain local worker. Do only the assigned subtask. "
    "Return compact JSON with keys answer, evidence, uncertainty. Evidence must cite paths or concrete inputs when available. "
    "Do not make architecture, security, destructive migration, or production-risk decisions."
)


@dataclass(slots=True)
class WorkerTask:
    task: str
    task_type: str = "general"
    context: str = ""


@dataclass(slots=True)
class WorkerResult:
    execution_id: str
    task: str
    ok: bool
    output: str
    latency_seconds: float
    input_tokens: int
    output_tokens: int
    error: str | None = None

    def to_dict(self) -> dict:
        return asdict(self)


class LocalWorkerPool:
    def __init__(self, config: BrainConfig, telemetry: TelemetryStore | None = None):
        self.config = config
        self.telemetry = telemetry or TelemetryStore(config)

    @staticmethod
    def _tokens(text: str) -> int:
        return max(1, len(text) // 4)

    def run_one(self, item: WorkerTask) -> WorkerResult:
        execution_id = self.telemetry.new_id()
        prompt = f"TASK TYPE: {item.task_type}\nTASK: {item.task}\n\nCONTEXT:\n{item.context}".strip()
        started = time.perf_counter()
        client = OllamaClient(self.config.ollama_url, self.config.local_model, timeout=self.config.local_worker_timeout_seconds)
        error = None
        output = ""
        try:
            output = client.generate(prompt, system=SYSTEM, num_ctx=self.config.local_worker_num_ctx)
            ok = True
        except OllamaError as exc:
            ok = False
            error = str(exc)
        latency = time.perf_counter() - started
        result = WorkerResult(
            execution_id=execution_id,
            task=item.task,
            ok=ok,
            output=output,
            latency_seconds=round(latency, 3),
            input_tokens=self._tokens(prompt),
            output_tokens=self._tokens(output) if output else 0,
            error=error,
        )
        self.telemetry.append(ExecutionRecord(
            execution_id=execution_id,
            task=item.task,
            task_type=item.task_type,
            model=self.config.local_model,
            route="local_worker",
            latency_seconds=result.latency_seconds,
            input_tokens=result.input_tokens,
            output_tokens=result.output_tokens,
            error=error,
        ))
        return result

    def run(self, tasks: list[WorkerTask], concurrency: int | None = None) -> list[WorkerResult]:
        if not tasks:
            return []
        workers = max(1, min(concurrency or self.config.local_worker_concurrency, len(tasks)))
        indexed: dict[object, int] = {}
        results: list[WorkerResult | None] = [None] * len(tasks)
        with ThreadPoolExecutor(max_workers=workers, thread_name_prefix="brain-local") as pool:
            for index, item in enumerate(tasks):
                indexed[pool.submit(self.run_one, item)] = index
            for future in as_completed(indexed):
                results[indexed[future]] = future.result()
        return [r for r in results if r is not None]

    @staticmethod
    def parse_task(value: str) -> WorkerTask:
        try:
            data = json.loads(value)
        except json.JSONDecodeError:
            return WorkerTask(task=value)
        if isinstance(data, dict) and "task" in data:
            return WorkerTask(
                task=str(data["task"]),
                task_type=str(data.get("task_type", "general")),
                context=str(data.get("context", "")),
            )
        return WorkerTask(task=value)
