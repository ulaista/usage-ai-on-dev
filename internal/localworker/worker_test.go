package localworker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ulaista/usage-ai-on-dev/internal/hardware"
)

type fakeDetector struct{ snapshot hardware.Snapshot }

func (f fakeDetector) Snapshot(context.Context) (hardware.Snapshot, error) { return f.snapshot, nil }

func TestWorkerRejectsUnsafeHardwareBeforeOllama(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()
	profile := hardware.SelectProfile("darwin", "Apple M2", 16*1024)
	worker := Worker{OllamaURL: server.URL, Detector: fakeDetector{snapshot: hardware.Snapshot{
		Profile: profile,
		Resources: hardware.RuntimeResources{AvailableMemoryMB: 6000, SwapUsedMB: 3500, MemoryPressure: "critical"},
	}}}
	result, err := worker.Run(context.Background(), Request{Task: "summarize diff", ContextTokens: 5000})
	if err != nil {
		t.Fatal(err)
	}
	if !result.FallbackRequired || called {
		t.Fatalf("expected hardware fallback without Ollama call, result=%#v called=%v", result, called)
	}
}

func TestWorkerReturnsTypedEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req["model"] != "qwen3.5:4b" {
			t.Fatalf("unexpected model: %#v", req["model"])
		}
		content := `{"answer":"auth changed","evidence":["auth.go"],"risks":[],"affected_symbols":["Login"],"verification":["go test ./..."],"uncertainty":0.1}`
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{"content": content},
			"prompt_eval_count": 320,
			"eval_count": 90,
		})
	}))
	defer server.Close()
	profile := hardware.SelectProfile("darwin", "Apple M3 Pro", 18*1024)
	worker := Worker{OllamaURL: server.URL, Detector: fakeDetector{snapshot: hardware.Snapshot{
		Profile: profile,
		Resources: hardware.RuntimeResources{AvailableMemoryMB: 13000, SwapUsedMB: 100, MemoryPressure: "normal"},
	}}}
	result, err := worker.Run(context.Background(), Request{Task: "summarize auth diff", Context: "auth.go", ContextTokens: 4000})
	if err != nil {
		t.Fatal(err)
	}
	if result.FallbackRequired {
		t.Fatalf("unexpected fallback: %#v", result)
	}
	if result.Evidence.Answer != "auth changed" || result.InputTokens != 320 || result.OutputTokens != 90 {
		t.Fatalf("unexpected worker result: %#v", result)
	}
}

func TestWorkerRequestsRecompileWhenContextExceedsSoftBudget(t *testing.T) {
	profile := hardware.SelectProfile("darwin", "Apple M2", 16*1024)
	worker := Worker{Detector: fakeDetector{snapshot: hardware.Snapshot{
		Profile: profile,
		Resources: hardware.RuntimeResources{AvailableMemoryMB: 12000, SwapUsedMB: 0, MemoryPressure: "normal"},
	}}}
	result, err := worker.Run(context.Background(), Request{Task: "summarize module", ContextTokens: 9000})
	if err != nil {
		t.Fatal(err)
	}
	if !result.FallbackRequired || result.Decision.RecommendedContextTokens != 6000 {
		t.Fatalf("expected context recompile request, got %#v", result)
	}
}
