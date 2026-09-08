package verification

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	contextpkg "github.com/ulaista/usage-ai-on-dev/internal/context"
	"github.com/ulaista/usage-ai-on-dev/internal/economy"
	"github.com/ulaista/usage-ai-on-dev/internal/localworker"
)

const CapsuleVersion = "verification-v1"

type Capsule struct {
	Version             string   `json:"version"`
	Task                string   `json:"task"`
	RepoStateID         string   `json:"repo_state_id,omitempty"`
	ContextFingerprint  string   `json:"context_fingerprint,omitempty"`
	WorkerExecutionID   string   `json:"worker_execution_id,omitempty"`
	WorkerModel         string   `json:"worker_model,omitempty"`
	Answer              string   `json:"answer"`
	Evidence            []string `json:"evidence,omitempty"`
	Risks               []string `json:"risks,omitempty"`
	AffectedSymbols     []string `json:"affected_symbols,omitempty"`
	Verification        []string `json:"verification,omitempty"`
	ChangedFiles        []string `json:"changed_files,omitempty"`
	RelevantFiles       []string `json:"relevant_files,omitempty"`
	Diff                string   `json:"diff,omitempty"`
	Uncertainty         float64  `json:"uncertainty"`
	FallbackRequired    bool     `json:"fallback_required"`
	FallbackReason      string   `json:"fallback_reason,omitempty"`
	EstimatedTokens     int      `json:"estimated_tokens"`
	FullContextTokens   int      `json:"full_context_tokens"`
	EstimatedTokenSaving int     `json:"estimated_token_saving"`
}

type BuildRequest struct {
	Task       string
	StateID    string
	ContextKey string
	Packet     contextpkg.Packet
	Result     localworker.Result
	MaxTokens  int
}

func Build(req BuildRequest) Capsule {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2500
	}
	capsule := Capsule{
		Version: CapsuleVersion,
		Task: req.Task,
		RepoStateID: req.StateID,
		ContextFingerprint: req.ContextKey,
		WorkerExecutionID: req.Result.ExecutionID,
		WorkerModel: req.Result.Model,
		Answer: req.Result.Evidence.Answer,
		Evidence: dedupe(req.Result.Evidence.Evidence, 16),
		Risks: dedupe(req.Result.Evidence.Risks, 10),
		AffectedSymbols: dedupe(req.Result.Evidence.AffectedSymbols, 16),
		Verification: dedupe(req.Result.Evidence.Verification, 12),
		ChangedFiles: dedupe(req.Packet.ChangedFiles, 20),
		RelevantFiles: relevantFiles(req.Packet, 20),
		Uncertainty: req.Result.Evidence.Uncertainty,
		FallbackRequired: req.Result.FallbackRequired,
		FallbackReason: req.Result.FallbackReason,
		FullContextTokens: req.Packet.EstimatedTokens,
	}
	capsule.Diff = trim(req.Packet.Diff, maxTokens*2)
	capsule.EstimatedTokens = estimate(capsule)
	if capsule.EstimatedTokens > maxTokens {
		capsule.Diff = trim(capsule.Diff, maxTokens)
		capsule.RelevantFiles = limit(capsule.RelevantFiles, 10)
		capsule.Evidence = limit(capsule.Evidence, 10)
		capsule.EstimatedTokens = estimate(capsule)
	}
	if capsule.EstimatedTokens > maxTokens {
		capsule.Diff = ""
		capsule.ChangedFiles = limit(capsule.ChangedFiles, 12)
		capsule.EstimatedTokens = estimate(capsule)
	}
	capsule.EstimatedTokenSaving = max(0, capsule.FullContextTokens-capsule.EstimatedTokens)
	return capsule
}

func (c Capsule) Fingerprint() string {
	data, _ := json.Marshal(c)
	return economy.Fingerprint(CapsuleVersion, string(data))
}

func (c Capsule) RenderMarkdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Verification Capsule\n\nTask: %s\nRepo state: %s\nWorker: %s\nEstimated tokens: %d\nFull context tokens avoided: %d\n\n", c.Task, c.RepoStateID, c.WorkerModel, c.EstimatedTokens, c.EstimatedTokenSaving)
	if c.Answer != "" { fmt.Fprintf(&b, "## Proposed result\n%s\n\n", c.Answer) }
	writeList(&b, "Evidence", c.Evidence)
	writeList(&b, "Affected symbols", c.AffectedSymbols)
	writeList(&b, "Risks", c.Risks)
	writeList(&b, "Verification", c.Verification)
	writeList(&b, "Relevant files", c.RelevantFiles)
	if c.Diff != "" { fmt.Fprintf(&b, "## Relevant current diff\n```diff\n%s\n```\n\n", c.Diff) }
	fmt.Fprintf(&b, "Uncertainty: %.2f\n", c.Uncertainty)
	if c.FallbackRequired { fmt.Fprintf(&b, "Fallback required: %s\n", c.FallbackReason) }
	return b.String()
}

func relevantFiles(packet contextpkg.Packet, n int) []string {
	seen := map[string]bool{}
	var out []string
	for _, entry := range packet.RepoMap.Entries {
		if !seen[entry.Path] { seen[entry.Path] = true; out = append(out, entry.Path) }
		if len(out) >= n { break }
	}
	return out
}

func dedupe(items []string, n int) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] { continue }
		seen[item] = true
		out = append(out, item)
	}
	sort.Strings(out)
	return limit(out, n)
}

func limit(items []string, n int) []string {
	if n <= 0 || len(items) <= n { return items }
	return items[:n]
}

func trim(value string, maxChars int) string {
	if maxChars <= 0 || len(value) <= maxChars { return value }
	return value[:maxChars] + "\n...truncated..."
}

func estimate(v any) int {
	data, _ := json.Marshal(v)
	if len(data) == 0 { return 0 }
	return max(1, len(data)/4)
}

func writeList(b *strings.Builder, title string, items []string) {
	if len(items) == 0 { return }
	fmt.Fprintf(b, "## %s\n", title)
	for _, item := range items { fmt.Fprintf(b, "- %s\n", item) }
	b.WriteString("\n")
}
