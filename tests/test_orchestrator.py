from project_brain.config import BrainConfig
from project_brain.orchestrator import DelegationOrchestrator


def _config(tmp_path):
    return BrainConfig(root=tmp_path, state_dir=tmp_path / ".project-brain")


def test_simple_documentation_task_delegates(tmp_path):
    plan = DelegationOrchestrator(_config(tmp_path)).plan(
        "summarize changed authentication files for documentation"
    )
    assert plan.action == "delegate_local_then_verify"
    assert plan.worker == "qwen3.5:4b"
    assert plan.estimated_strong_token_saving > 0


def test_security_architecture_stays_strong(tmp_path):
    plan = DelegationOrchestrator(_config(tmp_path)).plan(
        "redesign security architecture for authentication"
    )
    assert plan.action == "execute_strong"
    assert plan.worker is None


def test_delegation_can_be_disabled_when_overhead_is_too_high(tmp_path):
    config = _config(tmp_path)
    config.delegation_overhead_tokens = 10_000
    plan = DelegationOrchestrator(config).plan("summarize this file")
    assert plan.action == "execute_strong"
