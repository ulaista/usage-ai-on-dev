package contextpkg

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ulaista/usage-ai-on-dev/internal/config"
	"github.com/ulaista/usage-ai-on-dev/internal/core"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestCompileIncludesIntentAndCurrentDiff(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "brain@example.test")
	git(t, root, "config", "user.name", "Project Brain")
	path := filepath.Join(root, "auth.go")
	if err := os.WriteFile(path, []byte("package auth\n\nfunc Login() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "auth.go")
	git(t, root, "commit", "-m", "initial")
	if err := os.WriteFile(path, []byte("package auth\n\nfunc Login() {}\nfunc Logout() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default(root)
	svc, err := core.Open(context.Background(), cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	if _, err := svc.CreateIntent(context.Background(), "Auth update", "Add logout safely", nil, []string{"auth"}, []string{"tests"}); err != nil {
		t.Fatal(err)
	}

	packet, err := (Compiler{Service: svc}).Compile(context.Background(), "update auth logout", 4000)
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.ActiveIntents) != 1 {
		t.Fatalf("expected one active intent, got %#v", packet.ActiveIntents)
	}
	if !strings.Contains(packet.Diff, "Logout") {
		t.Fatalf("expected current diff, got %q", packet.Diff)
	}
	if len(packet.ChangedFiles) != 1 || packet.ChangedFiles[0] != "auth.go" {
		t.Fatalf("unexpected changed files: %#v", packet.ChangedFiles)
	}
	if !strings.Contains(packet.RenderMarkdown(), "Auth update") {
		t.Fatal("rendered context did not include active intent")
	}
}
