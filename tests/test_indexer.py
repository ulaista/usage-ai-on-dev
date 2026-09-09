from project_brain.config import BrainConfig
from project_brain.indexer import RepositoryIndexer


def test_incremental_index_detects_unchanged_files(tmp_path):
    source = tmp_path / "app.py"
    source.write_text("def hello():\n    return 'hi'\n", encoding="utf-8")
    config = BrainConfig(root=tmp_path, state_dir=tmp_path / ".project-brain")

    first = RepositoryIndexer(config).scan()
    assert first["stats"]["changed"] == ["app.py"]
    assert first["files"]["app.py"]["symbols"] == ["hello"]

    second = RepositoryIndexer(config).scan()
    assert second["stats"]["changed"] == []
    assert second["stats"]["unchanged_count"] == 1


def test_indexer_detects_deleted_files(tmp_path):
    source = tmp_path / "module.ts"
    source.write_text("export function run() {}\n", encoding="utf-8")
    config = BrainConfig(root=tmp_path, state_dir=tmp_path / ".project-brain")
    RepositoryIndexer(config).scan()
    source.unlink()

    result = RepositoryIndexer(config).scan()
    assert result["stats"]["deleted"] == ["module.ts"]
