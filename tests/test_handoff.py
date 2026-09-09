from project_brain.config import BrainConfig
from project_brain.handoff import HandoffManager


def test_handoff_threshold(tmp_path):
    config = BrainConfig(root=tmp_path, state_dir=tmp_path / ".project-brain")
    manager = HandoffManager(config)
    assert manager.should_handoff("continue task", 50_000) is True
    assert manager.should_handoff("continue task", 10_000) is False


def test_handoff_creates_markdown(tmp_path):
    config = BrainConfig(root=tmp_path, state_dir=tmp_path / ".project-brain")
    config.ensure_state()
    path = HandoffManager(config).create("continue auth refactor", 42_000, ["Tests are green"])
    text = path.read_text(encoding="utf-8")
    assert "Session Handoff" in text
    assert "Tests are green" in text
