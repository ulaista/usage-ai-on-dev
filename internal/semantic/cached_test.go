package semantic

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ulaista/usage-ai-on-dev/internal/domain"
)

type fakeProvider struct {
	symbolCalls    int
	referenceCalls int
	overviewCalls  int
}

func (f *fakeProvider) Name() string { return "fake" }
func (f *fakeProvider) Activate(context.Context, string) error { return nil }
func (f *fakeProvider) Close() error { return nil }
func (f *fakeProvider) SymbolsOverview(context.Context, string, int) (domain.SemanticResult, error) {
	f.overviewCalls++
	return domain.SemanticResult{Provider: f.Name(), Raw: "overview"}, nil
}
func (f *fakeProvider) FindSymbol(context.Context, domain.SymbolQuery) (domain.SemanticResult, error) {
	f.symbolCalls++
	return domain.SemanticResult{Provider: f.Name(), Raw: "symbol"}, nil
}
func (f *fakeProvider) FindReferences(context.Context, string, string) (domain.SemanticResult, error) {
	f.referenceCalls++
	return domain.SemanticResult{Provider: f.Name(), Raw: "references"}, nil
}

func gitCommand(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func semanticTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	gitCommand(t, root, "init")
	gitCommand(t, root, "config", "user.email", "brain@example.test")
	gitCommand(t, root, "config", "user.name", "Project Brain")
	if err := os.WriteFile(filepath.Join(root, "auth.go"), []byte("package auth\nfunc Login() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, root, "add", ".")
	gitCommand(t, root, "commit", "-m", "initial")
	return root
}

func TestCachedProviderReusesIdenticalQuery(t *testing.T) {
	root := semanticTestRepo(t)
	base := &fakeProvider{}
	cached := NewCachedProvider(base, root, filepath.Join(root, ".project-brain"), nil)
	query := domain.SymbolQuery{Pattern: "Login", RelativePath: "auth.go", IncludeBody: true, Depth: 1}

	first, err := cached.FindSymbol(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	second, err := cached.FindSymbol(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if first.CacheHit {
		t.Fatal("first query must not be a cache hit")
	}
	if !second.CacheHit {
		t.Fatal("second identical query must be a cache hit")
	}
	if base.symbolCalls != 1 {
		t.Fatalf("expected one provider call, got %d", base.symbolCalls)
	}
	if first.RepoStateID == "" || first.RepoStateID != second.RepoStateID {
		t.Fatalf("unexpected repo state ids: first=%q second=%q", first.RepoStateID, second.RepoStateID)
	}
}

func TestCachedProviderInvalidatesAfterSourceChange(t *testing.T) {
	root := semanticTestRepo(t)
	base := &fakeProvider{}
	cached := NewCachedProvider(base, root, filepath.Join(root, ".project-brain"), nil)
	query := domain.SymbolQuery{Pattern: "Login", RelativePath: "auth.go"}

	first, err := cached.FindSymbol(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "auth.go"), []byte("package auth\nfunc Login() {}\nfunc Logout() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := cached.FindSymbol(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if second.CacheHit {
		t.Fatal("source change must invalidate semantic cache")
	}
	if first.RepoStateID == second.RepoStateID {
		t.Fatalf("repo state did not change: %s", first.RepoStateID)
	}
	if base.symbolCalls != 2 {
		t.Fatalf("expected provider to run again after source change, got %d calls", base.symbolCalls)
	}
}

func TestCachedProviderSeparatesSemanticOperations(t *testing.T) {
	root := semanticTestRepo(t)
	base := &fakeProvider{}
	cached := NewCachedProvider(base, root, filepath.Join(root, ".project-brain"), nil)

	if _, err := cached.SymbolsOverview(context.Background(), "auth.go", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := cached.FindReferences(context.Background(), "Login", "auth.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := cached.SymbolsOverview(context.Background(), "auth.go", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := cached.FindReferences(context.Background(), "Login", "auth.go"); err != nil {
		t.Fatal(err)
	}
	if base.overviewCalls != 1 || base.referenceCalls != 1 {
		t.Fatalf("operations did not cache independently: overview=%d refs=%d", base.overviewCalls, base.referenceCalls)
	}
}
