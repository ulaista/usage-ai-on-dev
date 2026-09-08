from project_brain.config import BrainConfig
from project_brain.telemetry import AdaptivePolicy, ExecutionRecord, TelemetryStore


def make_config(tmp_path):
    return BrainConfig(root=tmp_path, state_dir=tmp_path / ".project-brain", adaptive_min_samples=3)


def test_telemetry_summary_and_mark(tmp_path):
    config = make_config(tmp_path)
    store = TelemetryStore(config)
    store.append(ExecutionRecord("EXE-1", "a", "summary", config.local_model, "local_worker", 2.0, 100, 20))
    store.append(ExecutionRecord("EXE-2", "b", "summary", config.local_model, "local_worker", 4.0, 120, 30))
    assert store.mark("EXE-1", True) is True
    stats = store.summary(config.local_model)
    assert stats["samples"] == 2
    assert stats["reviewed"] == 1
    assert stats["acceptance_rate"] == 1.0
    assert stats["avg_latency_seconds"] == 3.0


def test_adaptive_policy_rewards_reliable_local_model(tmp_path):
    config = make_config(tmp_path)
    store = TelemetryStore(config)
    for i in range(3):
        record = ExecutionRecord(f"EXE-{i}", "task", "summary", config.local_model, "local_worker", 2.0)
        store.append(record)
        store.mark(record.execution_id, True)
    policy = AdaptivePolicy(config, store)
    assert policy.local_multiplier() > 1.0
    assert policy.effective_local_complexity() > config.max_local_complexity


def test_adaptive_policy_penalizes_rejected_local_model(tmp_path):
    config = make_config(tmp_path)
    store = TelemetryStore(config)
    for i in range(3):
        record = ExecutionRecord(f"EXE-{i}", "task", "summary", config.local_model, "local_worker", 2.0)
        store.append(record)
        store.mark(record.execution_id, i == 0)
    policy = AdaptivePolicy(config, store)
    assert policy.local_multiplier() < 1.0
