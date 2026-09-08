package gitctx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil { t.Fatalf("git %v: %v: %s", args, err, out) }
}

func TestProjectBrainStateDoesNotInvalidateRepoState(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "brain@example.test")
	runGit(t, root, "config", "user.name", "Project Brain")
	if err := os.WriteFile(filepath.Join(root, "auth.go"), []byte("package auth\n"), 0o644); err != nil { t.Fatal(err) }
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")
	provider := Provider{Root: root}
	before, err := provider.StateID(context.Background())
	if err != nil { t.Fatal(err) }
	if err := os.MkdirAll(filepath.Join(root, ".project-brain"), 0o755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(root, ".project-brain", "brain.db"), []byte("state-1"), 0o644); err != nil { t.Fatal(err) }
	afterFirstWrite, err := provider.StateID(context.Background())
	if err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(root, ".project-brain", "brain.db"), []byte("state-2"), 0o644); err != nil { t.Fatal(err) }
	afterSecondWrite, err := provider.StateID(context.Background())
	if err != nil { t.Fatal(err) }
	if before != afterFirstWrite || before != afterSecondWrite { t.Fatalf("Project Brain internal state changed repo fingerprint: before=%s first=%s second=%s", before, afterFirstWrite, afterSecondWrite) }
	snapshot, err := provider.Snapshot(context.Background(), 10000)
	if err != nil { t.Fatal(err) }
	if len(snapshot.ChangedFiles) != 0 { t.Fatalf("Project Brain files leaked into changed files: %#v", snapshot.ChangedFiles) }
}
