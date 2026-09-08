package verification

import (
	"strings"
	"testing"

	contextpkg "github.com/ulaista/usage-ai-on-dev/internal/context"
	"github.com/ulaista/usage-ai-on-dev/internal/localworker"
	"github.com/ulaista/usage-ai-on-dev/internal/repomap"
)

func TestBuildCreatesCompactVerificationCapsule(t *testing.T) {
	packet := contextpkg.Packet{
		Task: "review auth change",
		EstimatedTokens: 12000,
		ChangedFiles: []string{"auth.go", "token/store.go"},
		RepoMap: repomap.Map{Entries: []repomap.Entry{{Path: "auth.go"}, {Path: "token/store.go"}}},
		Diff: strings.Repeat("+ changed auth line\n", 1200),
	}
	result := localworker.Result{
		ExecutionID: "EXE-test",
		Model: "qwen:test",
		Evidence: localworker.Evidence{
			Answer: "Auth validation remains correct.",
			Evidence: []string{"auth.go:10-30"},
			AffectedSymbols: []string{"ValidateToken"},
			Verification: []string{"go test ./..."},
			Uncertainty: 0.1,
		},
	}
	capsule := Build(BuildRequest{Task: packet.Task, StateID: "rs-test", ContextKey: "ctx-test", Packet: packet, Result: result, MaxTokens: 2500})
	if capsule.EstimatedTokens <= 0 || capsule.EstimatedTokens >= packet.EstimatedTokens {
		t.Fatalf("expected compact capsule, got capsule=%d full=%d", capsule.EstimatedTokens, packet.EstimatedTokens)
	}
	if capsule.EstimatedTokenSaving <= 0 {
		t.Fatalf("expected positive token saving, got %#v", capsule)
	}
	if !strings.Contains(capsule.RenderMarkdown(), "ValidateToken") {
		t.Fatal("verification markdown lost affected symbol evidence")
	}
	if capsule.Fingerprint() == "" {
		t.Fatal("expected stable verification fingerprint")
	}
}
