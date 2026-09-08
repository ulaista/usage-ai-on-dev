package repomap

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRepoMapRanksChangedAndDependentFiles(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "brain@example.test")
	git(t, root, "config", "user.name", "Project Brain")
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.25\n")
	write(t, filepath.Join(root, "auth", "service.go"), "package auth\n\nimport \"example.com/app/token\"\n\nfunc Login() {}\n")
	write(t, filepath.Join(root, "token", "store.go"), "package token\n\nfunc Rotate() {}\n")
	write(t, filepath.Join(root, "misc", "unused.go"), "package misc\n\nfunc Unused() {}\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial")
	write(t, filepath.Join(root, "auth", "service.go"), "package auth\n\nimport \"example.com/app/token\"\n\nfunc Login() {}\nfunc Logout() {}\n")

	builder := Builder{Root: root}
	result, err := builder.Build(context.Background(), Request{
		Task:         "update auth token login",
		ChangedFiles: []string{"auth/service.go"},
		TokenBudget:  1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) < 2 {
		t.Fatalf("expected ranked repo map, got %#v", result)
	}
	if result.Entries[0].Path != "auth/service.go" {
		t.Fatalf("expected changed auth file first, got %#v", result.Entries)
	}
	foundToken := false
	for _, entry := range result.Entries {
		if entry.Path == "token/store.go" {
			foundToken = true
			break
		}
	}
	if !foundToken {
		t.Fatalf("expected dependency target in repo map, got %#v", result.Entries)
	}
	if result.Edges == 0 {
		t.Fatal("expected dependency edge")
	}
}

func TestRepoMapReusesUnchangedCache(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "brain@example.test")
	git(t, root, "config", "user.name", "Project Brain")
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.25\n")
	write(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial")

	builder := Builder{Root: root}
	first, err := builder.Build(context.Background(), Request{Task: "main", TokenBudget: 500})
	if err != nil {
		t.Fatal(err)
	}
	if first.AnalyzedFiles == 0 {
		t.Fatal("expected first build to analyze files")
	}
	second, err := builder.Build(context.Background(), Request{Task: "main", TokenBudget: 500})
	if err != nil {
		t.Fatal(err)
	}
	if second.ReusedFiles == 0 {
		t.Fatalf("expected cache reuse, got %#v", second)
	}
}
