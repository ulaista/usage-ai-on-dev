package autotune

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ulaista/usage-ai-on-dev/internal/hardware"
)

type ModelInfo struct {
	Name          string `json:"name"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	ParameterSize string `json:"parameter_size,omitempty"`
	Quantization  string `json:"quantization,omitempty"`
	Family        string `json:"family,omitempty"`
}

type Trial struct {
	Model                string  `json:"model"`
	ContextTokens        int     `json:"context_tokens"`
	PromptTokens         int     `json:"prompt_tokens"`
	OutputTokens         int     `json:"output_tokens"`
	PromptTokensPerSec   float64 `json:"prompt_tokens_per_sec"`
	OutputTokensPerSec   float64 `json:"output_tokens_per_sec"`
	LoadMillis           float64 `json:"load_ms"`
	TotalMillis          float64 `json:"total_ms"`
	PeakMemoryDropMB     int     `json:"peak_memory_drop_mb"`
	PeakSwapGrowthMB     int     `json:"peak_swap_growth_mb"`
	WorstMemoryPressure  string  `json:"worst_memory_pressure"`
	Success              bool    `json:"success"`
	Error                string  `json:"error,omitempty"`
}

type ModelResult struct {
	Model           ModelInfo `json:"model"`
	Trials          []Trial   `json:"trials"`
	MaxSafeContext  int       `json:"max_safe_context_tokens"`
	MedianOutputTPS float64   `json:"median_output_tokens_per_sec"`
	MedianPromptTPS float64   `json:"median_prompt_tokens_per_sec"`
	Score           float64   `json:"score"`
	Usable          bool      `json:"usable"`
	Reason          string    `json:"reason,omitempty"`
}

type Recommendation struct {
	PreferredModel     string          `json:"preferred_model"`
	SoftContextTokens  int             `json:"soft_context_tokens"`
	HardContextTokens  int             `json:"hard_context_tokens"`
	MaxParallelWorkers int             `json:"max_parallel_workers"`
	Score              float64         `json:"score"`
	Reason             string          `json:"reason"`
	PolicyOverride     hardware.Limits `json:"policy_override"`
}

type Report struct {
	GeneratedAt   time.Time       `json:"generated_at"`
	Hardware      hardware.View   `json:"hardware"`
	Models        []ModelResult   `json:"models"`
	Recommendation Recommendation `json:"recommendation"`
	Warnings      []string        `json:"warnings,omitempty"`
}

type Options struct {
	Models       []string
	Contexts     []int
	MaxModels    int
	OutputTokens int
}

type Runner struct {
	OllamaURL  string
	StateDir   string
	Detector   hardware.Detector
	HTTPClient *http.Client
}

func (r *Runner) init() {
	if r.OllamaURL == "" {
		r.OllamaURL = "http://127.0.0.1:11434"
	}
	if r.Detector == nil {
		r.Detector = hardware.SystemDetector{}
	}
	if r.HTTPClient == nil {
		r.HTTPClient = &http.Client{Timeout: 4 * time.Minute}
	}
}

func (r *Runner) Run(ctx context.Context, opts Options) (Report, error) {
	r.init()
	snapshot, err := r.Detector.Snapshot(ctx)
	if err != nil {
		return Report{}, err
	}
	policy, err := hardware.LoadPolicy(hardware.PolicyPath(r.StateDir))
	if err != nil {
		return Report{}, err
	}
	view := hardware.BuildView(snapshot, policy)
	models, err := r.listModels(ctx)
	if err != nil {
		return Report{}, err
	}
	models = filterModels(models, opts.Models, opts.MaxModels)
	if len(models) == 0 {
		return Report{}, fmt.Errorf("no installed Ollama models selected for benchmark")
	}
	contexts := normalizeContexts(opts.Contexts, view.Effective)
	outputTokens := opts.OutputTokens
	if outputTokens <= 0 {
		outputTokens = 64
	}
	if outputTokens > 128 {
		outputTokens = 128
	}

	report := Report{GeneratedAt: time.Now().UTC(), Hardware: view}
	if view.RequiresUserAcceptance {
		report.Warnings = append(report.Warnings, "hardware policy has not been accepted; benchmark uses conservative recommended limits")
	}
	for _, model := range models {
		result := ModelResult{Model: model}
		for _, contextTokens := range contexts {
			trial := r.runTrial(ctx, model.Name, contextTokens, outputTokens, snapshot)
			result.Trials = append(result.Trials, trial)
		}
		finalizeModelResult(&result)
		report.Models = append(report.Models, result)
	}
	sort.SliceStable(report.Models, func(i, j int) bool { return report.Models[i].Score > report.Models[j].Score })
	report.Recommendation = recommend(report.Models, view.Effective)
	if report.Recommendation.PreferredModel == "" {
		report.Warnings = append(report.Warnings, "no model completed a safe benchmark trial; keep the current policy and use strong-model fallback")
	}
	return report, nil
}

func (r *Runner) listModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(r.OllamaURL, "/")+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama model discovery failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ollama model discovery returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Models []struct {
			Name    string `json:"name"`
			Model   string `json:"model"`
			Size    int64  `json:"size"`
			Details struct {
				Family            string `json:"family"`
				ParameterSize     string `json:"parameter_size"`
				QuantizationLevel string `json:"quantization_level"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode ollama model list: %w", err)
	}
	result := make([]ModelInfo, 0, len(payload.Models))
	for _, item := range payload.Models {
		name := item.Name
		if name == "" {
			name = item.Model
		}
		if name == "" {
			continue
		}
		result = append(result, ModelInfo{Name: name, SizeBytes: item.Size, ParameterSize: item.Details.ParameterSize, Quantization: item.Details.QuantizationLevel, Family: item.Details.Family})
	}
	return result, nil
}

func filterModels(models []ModelInfo, selected []string, maxModels int) []ModelInfo {
	wanted := map[string]bool{}
	for _, name := range selected {
		wanted[strings.TrimSpace(name)] = true
	}
	var out []ModelInfo
	for _, model := range models {
		if len(wanted) > 0 && !wanted[model.Name] {
			continue
		}
		out = append(out, model)
	}
	if maxModels <= 0 {
		maxModels = 4
	}
	if len(out) > maxModels {
		out = out[:maxModels]
	}
	return out
}

func normalizeContexts(requested []int, limits hardware.Limits) []int {
	candidates := requested
	if len(candidates) == 0 {
		candidates = []int{2048, 4096, limits.SoftContextTokens, limits.HardContextTokens}
	}
	seen := map[int]bool{}
	var out []int
	for _, value := range candidates {
		if value < 1024 || value > limits.HardContextTokens || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Ints(out)
	if len(out) == 0 {
		out = []int{min(4096, max(1024, limits.HardContextTokens))}
	}
	return out
}

type chatRequest struct {
	Model     string         `json:"model"`
	Stream    bool           `json:"stream"`
	Think     bool           `json:"think"`
	Messages  []chatMessage  `json:"messages"`
	Options   map[string]any `json:"options"`
	KeepAlive string         `json:"keep_alive"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	TotalDuration      int64 `json:"total_duration"`
	LoadDuration       int64 `json:"load_duration"`
	PromptEvalCount    int   `json:"prompt_eval_count"`
	PromptEvalDuration int64 `json:"prompt_eval_duration"`
	EvalCount          int   `json:"eval_count"`
	EvalDuration       int64 `json:"eval_duration"`
}

func (r *Runner) runTrial(ctx context.Context, model string, contextTokens, outputTokens int, baseline hardware.Snapshot) Trial {
	trial := Trial{Model: model, ContextTokens: contextTokens, WorstMemoryPressure: baseline.Resources.MemoryPressure}
	payload := chatRequest{
		Model: model,
		Stream: false,
		Think: false,
		KeepAlive: "0",
		Messages: []chatMessage{{Role: "user", Content: benchmarkPrompt()}},
		Options: map[string]any{"temperature": 0, "num_ctx": contextTokens, "num_predict": outputTokens},
	}
	data, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.OllamaURL, "/")+"/api/chat", bytes.NewReader(data))
	if err != nil {
		trial.Error = err.Error()
		return trial
	}
	req.Header.Set("Content-Type", "application/json")

	samplerCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	peakMemoryDrop := 0
	peakSwapGrowth := 0
	worstPressure := baseline.Resources.MemoryPressure
	var mu sync.Mutex
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-samplerCtx.Done():
				return
			case <-ticker.C:
				s, e := r.Detector.Snapshot(samplerCtx)
				if e != nil {
					continue
				}
				memoryDrop := max(0, baseline.Resources.AvailableMemoryMB-s.Resources.AvailableMemoryMB)
				swapGrowth := max(0, s.Resources.SwapUsedMB-baseline.Resources.SwapUsedMB)
				mu.Lock()
				if memoryDrop > peakMemoryDrop { peakMemoryDrop = memoryDrop }
				if swapGrowth > peakSwapGrowth { peakSwapGrowth = swapGrowth }
				if pressureRank(s.Resources.MemoryPressure) > pressureRank(worstPressure) { worstPressure = s.Resources.MemoryPressure }
				mu.Unlock()
			}
		}
	}()

	started := time.Now()
	resp, err := r.HTTPClient.Do(req)
	wall := time.Since(started)
	cancel()
	wg.Wait()
	mu.Lock()
	trial.PeakMemoryDropMB = peakMemoryDrop
	trial.PeakSwapGrowthMB = peakSwapGrowth
	trial.WorstMemoryPressure = worstPressure
	mu.Unlock()
	if err != nil {
		trial.Error = err.Error()
		trial.TotalMillis = float64(wall.Milliseconds())
		return trial
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		trial.Error = fmt.Sprintf("ollama returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
		trial.TotalMillis = float64(wall.Milliseconds())
		return trial
	}
	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		trial.Error = fmt.Sprintf("decode benchmark response: %v", err)
		return trial
	}
	trial.PromptTokens = parsed.PromptEvalCount
	trial.OutputTokens = parsed.EvalCount
	trial.PromptTokensPerSec = tokensPerSecond(parsed.PromptEvalCount, parsed.PromptEvalDuration)
	trial.OutputTokensPerSec = tokensPerSecond(parsed.EvalCount, parsed.EvalDuration)
	trial.LoadMillis = nanosToMillis(parsed.LoadDuration)
	trial.TotalMillis = nanosToMillis(parsed.TotalDuration)
	if trial.TotalMillis <= 0 { trial.TotalMillis = float64(wall.Milliseconds()) }
	trial.Success = parsed.EvalCount > 0 && pressureRank(trial.WorstMemoryPressure) < pressureRank("critical") && trial.PeakSwapGrowthMB < 1024
	if !trial.Success && trial.Error == "" {
		trial.Error = "trial exceeded safe runtime resource thresholds"
	}
	return trial
}

func benchmarkPrompt() string {
	return "Review this tiny Go function and answer with two concise bullet points: func add(a, b int) int { return a + b }. Mention correctness and one useful test."
}

func tokensPerSecond(tokens int, durationNS int64) float64 {
	if tokens <= 0 || durationNS <= 0 { return 0 }
	return float64(tokens) / (float64(durationNS) / 1e9)
}

func nanosToMillis(ns int64) float64 { return float64(ns) / 1e6 }

func finalizeModelResult(result *ModelResult) {
	var promptTPS, outputTPS []float64
	for _, trial := range result.Trials {
		if !trial.Success { continue }
		result.Usable = true
		if trial.ContextTokens > result.MaxSafeContext { result.MaxSafeContext = trial.ContextTokens }
		if trial.PromptTokensPerSec > 0 { promptTPS = append(promptTPS, trial.PromptTokensPerSec) }
		if trial.OutputTokensPerSec > 0 { outputTPS = append(outputTPS, trial.OutputTokensPerSec) }
	}
	if !result.Usable {
		result.Reason = "all trials failed or crossed safe resource thresholds"
		return
	}
	result.MedianPromptTPS = median(promptTPS)
	result.MedianOutputTPS = median(outputTPS)
	contextBonus := math.Log2(float64(max(2048, result.MaxSafeContext))/2048.0 + 1.0) * 5
	result.Score = result.MedianOutputTPS + result.MedianPromptTPS*0.08 + contextBonus
	if result.MedianOutputTPS < 8 { result.Score *= 0.55 }
}

func recommend(results []ModelResult, current hardware.Limits) Recommendation {
	for _, result := range results {
		if !result.Usable || result.MaxSafeContext <= 0 { continue }
		hard := min(current.HardContextTokens, result.MaxSafeContext)
		soft := min(current.SoftContextTokens, hard)
		if result.MedianOutputTPS >= 20 && result.MaxSafeContext > current.SoftContextTokens {
			soft = min(result.MaxSafeContext, current.HardContextTokens)
		}
		override := hardware.Limits{
			SoftContextTokens: soft,
			HardContextTokens: hard,
			MaxOutputTokens: current.MaxOutputTokens,
			MaxParallelWorkers: 1,
			PreferredModel: result.Model.Name,
			PreferredModelClass: current.PreferredModelClass,
		}
		return Recommendation{
			PreferredModel: result.Model.Name,
			SoftContextTokens: soft,
			HardContextTokens: hard,
			MaxParallelWorkers: 1,
			Score: result.Score,
			Reason: fmt.Sprintf("best measured safe score; median generation %.1f tok/s, prefill %.1f tok/s, safe context %d", result.MedianOutputTPS, result.MedianPromptTPS, result.MaxSafeContext),
			PolicyOverride: override,
		}
	}
	return Recommendation{}
}

func median(values []float64) float64 {
	if len(values) == 0 { return 0 }
	copyValues := append([]float64(nil), values...)
	sort.Float64s(copyValues)
	mid := len(copyValues)/2
	if len(copyValues)%2 == 1 { return copyValues[mid] }
	return (copyValues[mid-1]+copyValues[mid])/2
}

func pressureRank(value string) int {
	switch value {
	case "critical": return 3
	case "warning": return 2
	case "normal": return 1
	default: return 0
	}
}
