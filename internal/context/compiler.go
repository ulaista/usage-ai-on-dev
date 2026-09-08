package contextpkg

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ulaista/usage-ai-on-dev/internal/core"
	"github.com/ulaista/usage-ai-on-dev/internal/domain"
	"github.com/ulaista/usage-ai-on-dev/internal/gitctx"
	"github.com/ulaista/usage-ai-on-dev/internal/repomap"
)

type Packet struct {
	Task            string                  `json:"task"`
	EstimatedTokens int                     `json:"estimated_tokens"`
	ChangedFiles    []string                `json:"changed_files"`
	RecentCommits   []string                `json:"recent_commits"`
	ActiveIntents   []domain.Intent         `json:"active_intents"`
	RepoMap         repomap.Map             `json:"repo_map"`
	Semantic        []domain.SemanticResult `json:"semantic"`
	Diff            string                  `json:"diff,omitempty"`
	Warnings        []string                `json:"warnings,omitempty"`
}

type Compiler struct {
	Service *core.Service
}

var termRE = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]{2,}`)

func uniqueTerms(task string, limit int) []string {
	seen := map[string]bool{}
	var out []string
	for _, term := range termRE.FindAllString(task, -1) {
		lower := strings.ToLower(term)
		if seen[lower] || lower == "the" || lower == "and" || lower == "for" || lower == "with" || lower == "from" {
			continue
		}
		seen[lower] = true
		out = append(out, term)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func estimateTokens(v any) int {
	data, _ := json.Marshal(v)
	if len(data) == 0 {
		return 0
	}
	return max(1, len(data)/4)
}

func (c Compiler) Compile(ctx context.Context, task string, maxTokens int) (Packet, error) {
	if maxTokens <= 0 {
		maxTokens = c.Service.Config.TargetContext
	}
	packet := Packet{Task: task}
	git := gitctx.Provider{Root: c.Service.Config.Root}
	snapshot, err := git.Snapshot(ctx, maxTokens*6)
	if err != nil {
		packet.Warnings = append(packet.Warnings, err.Error())
	} else {
		packet.ChangedFiles = snapshot.ChangedFiles
		packet.RecentCommits = snapshot.RecentCommits
		packet.Diff = snapshot.Diff
	}

	intents, err := c.Service.Store.ListActiveIntents(ctx)
	if err != nil {
		return Packet{}, err
	}
	if len(intents) > 3 {
		intents = intents[:3]
	}
	packet.ActiveIntents = intents

	// Semantic queries seed the graph as well as provide precise symbol evidence.
	if c.Service.Semantic != nil {
		terms := uniqueTerms(task, 5)
		for _, term := range terms {
			result, err := c.Service.Semantic.FindSymbol(ctx, domain.SymbolQuery{Pattern: term, Depth: 1})
			if err != nil {
				packet.Warnings = append(packet.Warnings, fmt.Sprintf("semantic %s: %v", term, err))
				continue
			}
			packet.Semantic = append(packet.Semantic, result)
			if estimateTokens(packet.Semantic) >= maxTokens/4 {
				break
			}
		}
	}

	repoBudget := max(500, maxTokens/5)
	repo, err := (repomap.Builder{Root: c.Service.Config.Root}).Build(ctx, repomap.Request{
		Task:         task,
		ChangedFiles: packet.ChangedFiles,
		Semantic:     packet.Semantic,
		TokenBudget:  repoBudget,
	})
	if err != nil {
		packet.Warnings = append(packet.Warnings, "repo map: "+err.Error())
	} else {
		packet.RepoMap = repo
	}

	packet.EstimatedTokens = estimateTokens(packet)
	if packet.EstimatedTokens > maxTokens {
		packet.Warnings = append(packet.Warnings, "context exceeded target; dropping least critical sections")
		packet.Semantic = trimSemantic(packet.Semantic, maxTokens/5)
		packet.RecentCommits = trimStrings(packet.RecentCommits, 6)
		packet.Diff = trimString(packet.Diff, maxTokens*2)
		packet.EstimatedTokens = estimateTokens(packet)
	}
	if packet.EstimatedTokens > maxTokens {
		packet.Diff = trimString(packet.Diff, maxTokens)
		packet.Semantic = trimSemantic(packet.Semantic, maxTokens/8)
		packet.EstimatedTokens = estimateTokens(packet)
	}
	return packet, nil
}

func trimSemantic(items []domain.SemanticResult, tokenBudget int) []domain.SemanticResult {
	if tokenBudget <= 0 {
		return nil
	}
	var out []domain.SemanticResult
	used := 0
	for _, item := range items {
		cost := max(1, len(item.Raw)/4)
		if used+cost > tokenBudget {
			break
		}
		out = append(out, item)
		used += cost
	}
	return out
}

func trimStrings(items []string, limit int) []string {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func trimString(value string, maxChars int) string {
	if maxChars <= 0 || len(value) <= maxChars {
		return value
	}
	return value[:maxChars] + "\n...truncated..."
}

func (p Packet) RenderMarkdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Project Brain Context\n\nTask: %s\nEstimated tokens: %d\n\n", p.Task, p.EstimatedTokens)
	if len(p.ActiveIntents) > 0 {
		b.WriteString("## Active intents\n")
		for _, in := range p.ActiveIntents {
			fmt.Fprintf(&b, "- %s: %s | goal: %s\n", in.ID, in.Title, in.Goal)
		}
		b.WriteString("\n")
	}
	if len(p.ChangedFiles) > 0 {
		b.WriteString("## Current changed files\n")
		files := append([]string(nil), p.ChangedFiles...)
		sort.Strings(files)
		for _, file := range files {
			fmt.Fprintf(&b, "- %s\n", file)
		}
		b.WriteString("\n")
	}
	if repo := p.RepoMap.RenderMarkdown(); repo != "" {
		fmt.Fprintf(&b, "## Ranked repository map\n%s\n", repo)
	}
	if p.Diff != "" {
		fmt.Fprintf(&b, "## Current diff\n```diff\n%s\n```\n\n", p.Diff)
	}
	if len(p.Semantic) > 0 {
		b.WriteString("## Semantic evidence\n")
		for _, item := range p.Semantic {
			fmt.Fprintf(&b, "### %s\n%s\n\n", item.Provider, item.Raw)
		}
	}
	if len(p.RecentCommits) > 0 {
		b.WriteString("## Recent commits\n")
		for _, commit := range p.RecentCommits {
			fmt.Fprintf(&b, "- %s\n", commit)
		}
		b.WriteString("\n")
	}
	if len(p.Warnings) > 0 {
		b.WriteString("## Warnings\n")
		for _, warning := range p.Warnings {
			fmt.Fprintf(&b, "- %s\n", warning)
		}
	}
	return b.String()
}
