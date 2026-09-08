package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ulaista/usage-ai-on-dev/internal/domain"
)

func TestIntentAndTelemetryRoundTrip(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state", "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	intent := domain.Intent{
		ID: "INT-test", Title: "Refactor auth", Goal: "Keep API stable", Status: "active",
		Constraints: []string{"no breaking API"}, Affected: []string{"auth"}, Verification: []string{"tests"},
		CreatedAt: now, UpdatedAt: now,
	}
	if err := st.CreateIntent(ctx, intent); err != nil {
		t.Fatal(err)
	}
	intents, err := st.ListActiveIntents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 1 || intents[0].ID != intent.ID || len(intents[0].Constraints) != 1 {
		t.Fatalf("unexpected intents: %#v", intents)
	}

	accepted := true
	if err := st.RecordExecution(ctx, domain.Execution{
		ID: "EXE-test", Task: "summarize auth", TaskType: "summary", Model: "qwen3.5:4b", Route: "local",
		LatencyMillis: 1200, InputTokens: 100, OutputTokens: 20, Accepted: &accepted, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	stats, err := st.TelemetrySummary(ctx, "qwen3.5:4b")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Samples != 1 || stats.Reviewed != 1 || stats.AcceptanceRate != 1 || stats.InputTokens != 100 {
		t.Fatalf("unexpected telemetry: %#v", stats)
	}
}
