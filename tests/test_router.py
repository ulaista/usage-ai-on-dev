from project_brain.config import BrainConfig
from project_brain.models import Complexity
from project_brain.router import ModelRouter


def test_low_complexity_routes_local(tmp_path):
    router = ModelRouter(BrainConfig(root=tmp_path, state_dir=tmp_path / ".project-brain"))
    decision = router.decide(Complexity(scope=1, ambiguity=1, unknown_code=1))
    assert decision.target == "local"
    assert decision.allow_fallback is False


def test_security_routes_strong(tmp_path):
    router = ModelRouter(BrainConfig(root=tmp_path, state_dir=tmp_path / ".project-brain"))
    complexity = Complexity(scope=1, security=4, flags={"security"})
    decision = router.decide(complexity)
    assert decision.target == "strong"
    assert "security" in decision.reason


def test_medium_complexity_allows_fallback(tmp_path):
    router = ModelRouter(BrainConfig(root=tmp_path, state_dir=tmp_path / ".project-brain"))
    decision = router.decide(Complexity(scope=3, ambiguity=2, cross_module=3))
    assert decision.target == "local"
    assert decision.allow_fallback is True
