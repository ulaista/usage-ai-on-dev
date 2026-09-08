from project_brain.config import BrainConfig
from project_brain.telemetry import TelemetryStore
from project_brain.worker import LocalWorkerPool, WorkerTask


def test_worker_pool_runs_tasks_and_records_telemetry(tmp_path, monkeypatch):
    config = BrainConfig(root=tmp_path, state_dir=tmp_path / ".project-brain", local_worker_concurrency=2)

    def fake_generate(self, prompt, *, system=None, num_ctx=32768):
        return '{"answer":"ok","evidence":[],"uncertainty":0.1}'

    monkeypatch.setattr("project_brain.worker.OllamaClient.generate", fake_generate)
    pool = LocalWorkerPool(config)
    results = pool.run([
        WorkerTask("summarize a", "summary", "a"),
        WorkerTask("summarize b", "summary", "b"),
    ])
    assert len(results) == 2
    assert all(item.ok for item in results)
    assert all(item.execution_id.startswith("EXE-") for item in results)
    assert TelemetryStore(config).summary(config.local_model)["samples"] == 2


def test_parse_task_accepts_json(tmp_path):
    config = BrainConfig(root=tmp_path, state_dir=tmp_path / ".project-brain")
    task = LocalWorkerPool(config).parse_task('{"task":"docs","task_type":"documentation","context":"diff"}')
    assert task.task == "docs"
    assert task.task_type == "documentation"
    assert task.context == "diff"
