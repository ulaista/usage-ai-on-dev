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

func TestCompileIncludesIntentDiffAndRankedRepoMap(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "brain@example.test")
	git(t, root, "config", "user.name", "Project Brain")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "token"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "token", "store.go"), []byte("package token\n\nfunc Rotate() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "auth.go")
	if err := os.WriteFile(path, []byte("package auth\n\nimport \"example.com/app/token\"\n\nfunc Login() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial")
	if err := os.WriteFile(path, []byte("package auth\n\nimport \"example.com/app/token\"\n\nfunc Login() {}\nfunc Logout() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default(root)
	cfg.RepoMapTokens = 1000
	svc, err := core.Open(context.Background(), cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	if _, err := svc.CreateIntent(context.Background(), "Auth update", "Add logout safely", nil, []string{"auth"}, []string{"tests"}); err != nil {
		t.Fatal(err)
	}

	packet, err := (Compiler{Service: svc}).Compile(context.Background(), "update auth token logout", 4000)
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
	if len(packet.RepoMap.Entries) < 2 {
		t.Fatalf("expected ranked repository context, got %#v", packet.RepoMap)
	}
	markdown := packet.RenderMarkdown()
	if !strings.Contains(markdown, "Auth update") {
		t.Fatal("rendered context did not include active intent")
	}
	if !strings.Contains(markdown, "token/store.go") {
		t.Fatalf("rendered context did not include dependency target:\n%s", markdown)
	}
	if !strings.Contains(markdown, "Ranked repository map") {
		t.Fatal("rendered context did not include repo-map section")
	}
}
