package localworker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ulaista/usage-ai-on-dev/internal/domain"
	"github.com/ulaista/usage-ai-on-dev/internal/hardware"
	"github.com/ulaista/usage-ai-on-dev/internal/store"
)

type Evidence struct {
	Answer          string   `json:"answer"`
	Evidence        []string `json:"evidence,omitempty"`
	Risks           []string `json:"risks,omitempty"`
	AffectedSymbols []string `json:"affected_symbols,omitempty"`
	Verification    []string `json:"verification,omitempty"`
	Uncertainty     float64  `json:"uncertainty"`
}

type Request struct {
	Task          string `json:"task"`
	TaskType      string `json:"task_type,omitempty"`
	Context       string `json:"context,omitempty"`
	ContextTokens int    `json:"context_tokens,omitempty"`
}

type Result struct {
	ExecutionID     string              `json:"execution_id"`
	Model           string              `json:"model"`
	Hardware        hardware.Snapshot   `json:"hardware"`
	Decision        hardware.Decision   `json:"decision"`
	Evidence        Evidence            `json:"evidence"`
	InputTokens     int                 `json:"input_tokens"`
	OutputTokens    int                 `json:"output_tokens"`
	LatencyMillis   int64               `json:"latency_ms"`
	FallbackRequired bool               `json:"fallback_required"`
	FallbackReason  string              `json:"fallback_reason,omitempty"`
}

type Worker struct {
	Model      string
	OllamaURL  string
	Detector   hardware.Detector
	Store      *store.Store
	HTTPClient *http.Client
	sem        chan struct{}
	once       sync.Once
}

func (w *Worker) init() {
	w.once.Do(func() {
		w.sem = make(chan struct{}, 1)
		if w.Detector == nil {
			w.Detector = hardware.SystemDetector{}
		}
		if w.HTTPClient == nil {
			w.HTTPClient = &http.Client{Timeout: 3 * time.Minute}
		}
		if w.OllamaURL == "" {
			w.OllamaURL = "http://127.0.0.1:11434"
		}
	})
}

func (w *Worker) Run(ctx context.Context, req Request) (Result, error) {
	w.init()
	if strings.TrimSpace(req.Task) == "" {
		return Result{}, fmt.Errorf("task is required")
	}

	snapshot, err := w.Detector.Snapshot(ctx)
	if err != nil {
		return Result{}, err
	}
	decision := snapshot.Profile.Evaluate(snapshot.Resources, req.ContextTokens)
	result := Result{Model: w.Model, Hardware: snapshot, Decision: decision}
	if !decision.Allowed {
		result.FallbackRequired = true
		result.FallbackReason = decision.Reason
		_ = w.record(ctx, req, result, decision.Reason)
		return result, nil
	}
	if req.ContextTokens > decision.RecommendedContextTokens && decision.RecommendedContextTokens > 0 {
		result.FallbackRequired = true
		result.FallbackReason = "context must be recompiled to the recommended local budget before execution"
		_ = w.record(ctx, req, result, result.FallbackReason)
		return result, nil
	}

	select {
	case w.sem <- struct{}{}:
		defer func() { <-w.sem }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}

	start := time.Now()
	response, err := w.callOllama(ctx, req, snapshot.Profile)
	result.LatencyMillis = time.Since(start).Milliseconds()
	if err != nil {
		result.FallbackRequired = true
		result.FallbackReason = err.Error()
		_ = w.record(ctx, req, result, err.Error())
		return result, nil
	}
	result.Evidence = response.Evidence
	result.InputTokens = response.PromptTokens
	result.OutputTokens = response.OutputTokens
	_ = w.record(ctx, req, result, "")
	return result, nil
}

type ollamaRequest struct {
	Model     string         `json:"model"`
	Stream    bool           `json:"stream"`
	Format    string         `json:"format"`
	Messages  []ollamaMessage `json:"messages"`
	Options   map[string]any `json:"options,omitempty"`
	KeepAlive string         `json:"keep_alive,omitempty"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	PromptEvalCount int `json:"prompt_eval_count"`
	EvalCount       int `json:"eval_count"`
}

type workerResponse struct {
	Evidence     Evidence
	PromptTokens int
	OutputTokens int
}

func (w *Worker) callOllama(ctx context.Context, req Request, profile hardware.Profile) (workerResponse, error) {
	model := w.Model
	if model == "" {
		model = "qwen3.5:4b"
	}
	system := `You are Project Brain's bounded local worker. Do only the requested low-risk task. Do not make architecture, security, destructive migration, breaking API, or production-risk decisions. Return JSON with answer, evidence, risks, affected_symbols, verification, uncertainty. uncertainty is 0 to 1.`
	user := "TASK:\n" + req.Task
	if req.Context != "" {
		user += "\n\nBOUNDED CONTEXT:\n" + req.Context
	}
	payload := ollamaRequest{
		Model: model, Stream: false, Format: "json", KeepAlive: "2m",
		Messages: []ollamaMessage{{Role: "system", Content: system}, {Role: "user", Content: user}},
		Options: map[string]any{"temperature": 0, "num_ctx": profile.HardContextTokens, "num_predict": profile.MaxOutputTokens},
	}
	data, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(w.OllamaURL, "/")+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return workerResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := w.HTTPClient.Do(httpReq)
	if err != nil {
		return workerResponse{}, fmt.Errorf("ollama unavailable: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return workerResponse{}, fmt.Errorf("ollama returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var parsed ollamaResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return workerResponse{}, fmt.Errorf("decode ollama response: %w", err)
	}
	var evidence Evidence
	if err := json.Unmarshal([]byte(parsed.Message.Content), &evidence); err != nil {
		return workerResponse{}, fmt.Errorf("local worker returned invalid evidence JSON: %w", err)
	}
	if evidence.Uncertainty < 0 || evidence.Uncertainty > 1 {
		return workerResponse{}, fmt.Errorf("local worker uncertainty must be between 0 and 1")
	}
	return workerResponse{Evidence: evidence, PromptTokens: parsed.PromptEvalCount, OutputTokens: parsed.EvalCount}, nil
}

func (w *Worker) record(ctx context.Context, req Request, result Result, errText string) error {
	if w.Store == nil {
		return nil
	}
	id := fmt.Sprintf("EXE-%d", time.Now().UnixNano())
	result.ExecutionID = id
	return w.Store.RecordExecution(ctx, domain.Execution{
		ID: id, Task: req.Task, TaskType: req.TaskType, Model: result.Model, Route: "local",
		LatencyMillis: result.LatencyMillis, InputTokens: result.InputTokens, OutputTokens: result.OutputTokens,
		Fallback: result.FallbackRequired, Error: errText, CreatedAt: time.Now().UTC(),
	})
}
