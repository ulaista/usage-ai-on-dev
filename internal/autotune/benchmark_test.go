package autotune

import (
	"testing"

	"github.com/ulaista/usage-ai-on-dev/internal/hardware"
)

func TestFinalizeModelResultRanksSafeTrials(t *testing.T) {
	result := ModelResult{Model: ModelInfo{Name: "qwen:test"}, Trials: []Trial{
		{ContextTokens: 4096, PromptTokensPerSec: 120, OutputTokensPerSec: 30, Success: true},
		{ContextTokens: 8192, PromptTokensPerSec: 100, OutputTokensPerSec: 24, Success: true},
		{ContextTokens: 12000, Success: false, Error: "pressure"},
	}}
	finalizeModelResult(&result)
	if !result.Usable {
		t.Fatal("expected model usable")
	}
	if result.MaxSafeContext != 8192 {
		t.Fatalf("expected 8192 safe context, got %d", result.MaxSafeContext)
	}
	if result.Score <= 0 {
		t.Fatalf("expected positive score, got %f", result.Score)
	}
}

func TestRecommendPrefersHighestScoredSafeModel(t *testing.T) {
	results := []ModelResult{
		{Model: ModelInfo{Name: "fast:4b"}, Usable: true, MaxSafeContext: 8192, MedianOutputTPS: 35, MedianPromptTPS: 100, Score: 50},
		{Model: ModelInfo{Name: "slow:8b"}, Usable: true, MaxSafeContext: 12000, MedianOutputTPS: 12, MedianPromptTPS: 50, Score: 20},
	}
	limits := hardware.Limits{SoftContextTokens: 6000, HardContextTokens: 12000, MaxOutputTokens: 1000, MaxParallelWorkers: 1, PreferredModelClass: "4b-8b-q4"}
	rec := recommend(results, limits)
	if rec.PreferredModel != "fast:4b" {
		t.Fatalf("expected fast:4b, got %s", rec.PreferredModel)
	}
	if rec.HardContextTokens != 8192 {
		t.Fatalf("expected measured hard context 8192, got %d", rec.HardContextTokens)
	}
}

func TestValidateReportRejectsDifferentHardware(t *testing.T) {
	report := Report{
		Hardware: hardware.View{Snapshot: hardware.Snapshot{Inventory: hardware.Inventory{HardwareID: "old"}}},
		Recommendation: Recommendation{PreferredModel: "qwen:test"},
	}
	if err := ValidateReportForHardware(report, "new"); err == nil {
		t.Fatal("expected hardware mismatch error")
	}
}

func TestNormalizeContextsNeverExceedsHardLimit(t *testing.T) {
	limits := hardware.Limits{SoftContextTokens: 6000, HardContextTokens: 12000}
	values := normalizeContexts([]int{512, 2048, 8192, 16000}, limits)
	for _, value := range values {
		if value > 12000 {
			t.Fatalf("context %d exceeds hard limit", value)
		}
	}
}
