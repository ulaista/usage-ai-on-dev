package contextpkg

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ulaista/usage-ai-on-dev/internal/config"
	"github.com/ulaista/usage-ai-on-dev/internal/core"
)

func TestCompileDeltaSendsFullThenNoChangeThenDelta(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "brain@example.test")
	git(t, root, "config", "user.name", "Project Brain")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.25\n"), 0o644); err != nil { t.Fatal(err) }
	path := filepath.Join(root, "auth.go")
	if err := os.WriteFile(path, []byte("package auth\n\nfunc Login() {}\n"), 0o644); err != nil { t.Fatal(err) }
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial")

	cfg := config.Default(root)
	cfg.RepoMapTokens = 800
	svc, err := core.Open(context.Background(), cfg, false)
	if err != nil { t.Fatal(err) }
	defer svc.Close()
	compiler := Compiler{Service: svc}

	first, err := compiler.CompileDelta(context.Background(), "session-1", "review auth", 4000)
	if err != nil { t.Fatal(err) }
	if !first.Full || first.FullPacket == nil { t.Fatalf("first delta must establish a full baseline: %#v", first) }

	second, err := compiler.CompileDelta(context.Background(), "session-1", "review auth", 4000)
	if err != nil { t.Fatal(err) }
	if second.Full || !second.NoChanges { t.Fatalf("second unchanged call should be compact no-change delta: %#v", second) }
	if second.EstimatedTokenSaving <= 0 { t.Fatalf("expected no-change delta to avoid full context: %#v", second) }

	if err := os.WriteFile(path, []byte("package auth\n\nfunc Login() {}\nfunc Logout() {}\n"), 0o644); err != nil { t.Fatal(err) }
	third, err := compiler.CompileDelta(context.Background(), "session-1", "review auth", 4000)
	if err != nil { t.Fatal(err) }
	if third.Full || third.NoChanges { t.Fatalf("changed repo should produce a delta: %#v", third) }
	if third.BaseStateID == third.CurrentStateID { t.Fatalf("repo state should change after source edit: %#v", third) }
	if third.EstimatedTokens >= third.FullContextTokens && third.FullContextTokens > 0 { t.Fatalf("delta should be smaller than full context: %#v", third) }
}
