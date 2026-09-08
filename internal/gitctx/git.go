package gitctx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Snapshot struct {
	StateID       string   `json:"state_id"`
	ChangedFiles  []string `json:"changed_files"`
	Diff          string   `json:"diff"`
	RecentCommits []string `json:"recent_commits"`
}

type Provider struct {
	Root string
}

func (p Provider) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", p.Root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func lines(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, "\n")
	out := make([]string, 0, len(parts))
	for _, item := range parts {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func (p Provider) StateID(ctx context.Context) (string, error) {
	head, err := p.run(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	status, err := p.run(ctx, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return "", err
	}
	diff, err := p.run(ctx, "diff", "--binary", "HEAD")
	if err != nil {
		return "", err
	}
	untrackedRaw, _ := p.run(ctx, "ls-files", "--others", "--exclude-standard")
	untracked := lines(untrackedRaw)
	sort.Strings(untracked)
	h := sha256.New()
	_, _ = h.Write([]byte("project-brain-repo-state-v1\n" + head + "\n" + status + "\n" + diff + "\n"))
	for _, relative := range untracked {
		clean := filepath.Clean(relative)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			continue
		}
		_, _ = h.Write([]byte(relative + "\x00"))
		data, readErr := os.ReadFile(filepath.Join(p.Root, clean))
		if readErr == nil {
			_, _ = h.Write(data)
		}
		_, _ = h.Write([]byte("\x00"))
	}
	return "rs-" + hex.EncodeToString(h.Sum(nil)[:12]), nil
}

func (p Provider) Snapshot(ctx context.Context, maxDiffChars int) (Snapshot, error) {
	stateID, err := p.StateID(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	changed, err := p.run(ctx, "diff", "--name-only", "HEAD")
	if err != nil {
		return Snapshot{}, err
	}
	untracked, _ := p.run(ctx, "ls-files", "--others", "--exclude-standard")
	changedFiles := append(lines(changed), lines(untracked)...)
	seen := map[string]bool{}
	unique := changedFiles[:0]
	for _, file := range changedFiles {
		if !seen[file] {
			seen[file] = true
			unique = append(unique, file)
		}
	}
	sort.Strings(unique)
	diff, err := p.run(ctx, "diff", "--unified=1", "HEAD")
	if err != nil {
		return Snapshot{}, err
	}
	if maxDiffChars > 0 && len(diff) > maxDiffChars {
		diff = diff[:maxDiffChars] + "\n...diff truncated by Project Brain..."
	}
	logText, err := p.run(ctx, "log", "--oneline", "-n", "12")
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{StateID: stateID, ChangedFiles: unique, Diff: diff, RecentCommits: lines(logText)}, nil
}
