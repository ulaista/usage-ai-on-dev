package contextpkg

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ulaista/usage-ai-on-dev/internal/domain"
	"github.com/ulaista/usage-ai-on-dev/internal/economy"
)

const deltaVersion = "context-delta-v1"

type Delta struct {
	Version              string   `json:"version"`
	SessionID            string   `json:"session_id"`
	Task                 string   `json:"task"`
	BaseStateID          string   `json:"base_state_id,omitempty"`
	CurrentStateID       string   `json:"current_state_id"`
	BaseContextKey       string   `json:"base_context_key,omitempty"`
	CurrentContextKey    string   `json:"current_context_key"`
	Full                 bool     `json:"full"`
	NoChanges            bool     `json:"no_changes"`
	AddedChangedFiles    []string `json:"added_changed_files,omitempty"`
	RemovedChangedFiles  []string `json:"removed_changed_files,omitempty"`
	AddedRelevantFiles   []string `json:"added_relevant_files,omitempty"`
	RemovedRelevantFiles []string `json:"removed_relevant_files,omitempty"`
	IntentChanges        []string `json:"intent_changes,omitempty"`
	Diff                 string   `json:"diff,omitempty"`
	FullPacket           *Packet  `json:"full_packet,omitempty"`
	EstimatedTokens      int      `json:"estimated_tokens"`
	FullContextTokens    int      `json:"full_context_tokens"`
	EstimatedTokenSaving int      `json:"estimated_token_saving"`
}

type sessionBaseline struct {
	Task       string `json:"task"`
	StateID    string `json:"state_id"`
	ContextKey string `json:"context_key"`
	Packet     Packet `json:"packet"`
}

func (c Compiler) CompileDelta(ctx context.Context, sessionID, task string, maxTokens int) (Delta, error) {
	if strings.TrimSpace(sessionID) == "" { return Delta{}, fmt.Errorf("session_id is required") }
	cached, err := c.CompileCached(ctx, task, maxTokens)
	if err != nil { return Delta{}, err }
	current := sessionBaseline{Task: economy.NormalizeTask(task), StateID: cached.StateID, ContextKey: cached.Key, Packet: cached.Packet}
	key := economy.Fingerprint(deltaVersion, sessionID)
	cache := economy.FileCache{Dir: filepath.Join(c.Service.Config.StateDir, "cache")}
	var previous sessionBaseline
	hit, loadErr := cache.Load("context-sessions", key, &previous)
	if loadErr != nil { return Delta{}, loadErr }

	delta := Delta{
		Version: deltaVersion, SessionID: sessionID, Task: task,
		CurrentStateID: current.StateID, CurrentContextKey: current.ContextKey,
		FullContextTokens: current.Packet.EstimatedTokens,
	}
	if !hit || previous.Task != current.Task {
		delta.Full = true
		packet := current.Packet
		delta.FullPacket = &packet
		delta.EstimatedTokens = current.Packet.EstimatedTokens
		if err := cache.Save("context-sessions", key, current); err != nil { return Delta{}, err }
		return delta, nil
	}

	delta.BaseStateID = previous.StateID
	delta.BaseContextKey = previous.ContextKey
	if previous.ContextKey == current.ContextKey {
		delta.NoChanges = true
		delta.EstimatedTokens = estimateDelta(delta)
		delta.EstimatedTokenSaving = max(0, delta.FullContextTokens-delta.EstimatedTokens)
		_ = c.Service.Store.RecordSaving(ctx, domain.CacheSaving{Kind: "delta", Key: current.ContextKey, SavedInputTokens: delta.EstimatedTokenSaving, CreatedAt: time.Now().UTC()})
		return delta, nil
	}

	delta.AddedChangedFiles, delta.RemovedChangedFiles = stringDelta(previous.Packet.ChangedFiles, current.Packet.ChangedFiles)
	delta.AddedRelevantFiles, delta.RemovedRelevantFiles = stringDelta(repoPaths(previous.Packet), repoPaths(current.Packet))
	delta.IntentChanges = intentDelta(previous.Packet.ActiveIntents, current.Packet.ActiveIntents)
	delta.Diff = trimDelta(current.Packet.Diff, max(2000, maxTokens*2))
	delta.EstimatedTokens = estimateDelta(delta)
	if maxTokens > 0 && delta.EstimatedTokens > maxTokens/2 {
		delta.Diff = trimDelta(delta.Diff, maxTokens)
		delta.AddedRelevantFiles = limitDelta(delta.AddedRelevantFiles, 12)
		delta.RemovedRelevantFiles = limitDelta(delta.RemovedRelevantFiles, 12)
		delta.EstimatedTokens = estimateDelta(delta)
	}
	delta.EstimatedTokenSaving = max(0, delta.FullContextTokens-delta.EstimatedTokens)
	if delta.EstimatedTokenSaving > 0 {
		_ = c.Service.Store.RecordSaving(ctx, domain.CacheSaving{Kind: "delta", Key: current.ContextKey, SavedInputTokens: delta.EstimatedTokenSaving, CreatedAt: time.Now().UTC()})
	}
	if err := cache.Save("context-sessions", key, current); err != nil { return Delta{}, err }
	return delta, nil
}

func (d Delta) RenderMarkdown() string {
	if d.Full && d.FullPacket != nil { return d.FullPacket.RenderMarkdown() }
	var b strings.Builder
	fmt.Fprintf(&b, "# Project Brain Context Delta\n\nSession: %s\nTask: %s\nBase state: %s\nCurrent state: %s\nEstimated tokens: %d\nEstimated full-context tokens avoided: %d\n\n", d.SessionID, d.Task, d.BaseStateID, d.CurrentStateID, d.EstimatedTokens, d.EstimatedTokenSaving)
	if d.NoChanges { b.WriteString("No relevant context changes since the previous capsule.\n"); return b.String() }
	writeDeltaList(&b, "Changed files added", d.AddedChangedFiles)
	writeDeltaList(&b, "Changed files removed", d.RemovedChangedFiles)
	writeDeltaList(&b, "Relevant files added", d.AddedRelevantFiles)
	writeDeltaList(&b, "Relevant files removed", d.RemovedRelevantFiles)
	writeDeltaList(&b, "Intent changes", d.IntentChanges)
	if d.Diff != "" { fmt.Fprintf(&b, "## Current diff delta\n```diff\n%s\n```\n", d.Diff) }
	return b.String()
}

func repoPaths(packet Packet) []string {
	out := make([]string, 0, len(packet.RepoMap.Entries))
	for _, entry := range packet.RepoMap.Entries { out = append(out, entry.Path) }
	return out
}

func stringDelta(before, after []string) (added, removed []string) {
	b := map[string]bool{}; a := map[string]bool{}
	for _, item := range before { b[item] = true }
	for _, item := range after { a[item] = true }
	for item := range a { if !b[item] { added = append(added, item) } }
	for item := range b { if !a[item] { removed = append(removed, item) } }
	sort.Strings(added); sort.Strings(removed)
	return
}

func intentDelta(before, after []domain.Intent) []string {
	encode := func(items []domain.Intent) map[string]string {
		out := map[string]string{}
		for _, item := range items { data, _ := json.Marshal(item); out[item.ID] = string(data) }
		return out
	}
	b, a := encode(before), encode(after)
	var changes []string
	for id, value := range a {
		if old, ok := b[id]; !ok { changes = append(changes, "added intent "+id) } else if old != value { changes = append(changes, "updated intent "+id) }
	}
	for id := range b { if _, ok := a[id]; !ok { changes = append(changes, "removed intent "+id) } }
	sort.Strings(changes)
	return changes
}

func estimateDelta(v any) int { data, _ := json.Marshal(v); if len(data)==0 { return 0 }; return max(1, len(data)/4) }
func trimDelta(value string, maxChars int) string { if maxChars<=0 || len(value)<=maxChars { return value }; return value[:maxChars]+"\n...truncated..." }
func limitDelta(items []string, n int) []string { if n<=0 || len(items)<=n { return items }; return items[:n] }
func writeDeltaList(b *strings.Builder, title string, items []string) { if len(items)==0 { return }; fmt.Fprintf(b,"## %s\n",title); for _, item := range items { fmt.Fprintf(b,"- %s\n",item) }; b.WriteString("\n") }
