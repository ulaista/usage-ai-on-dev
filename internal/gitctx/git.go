package gitctx

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Snapshot struct {
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

func (p Provider) Snapshot(ctx context.Context, maxDiffChars int) (Snapshot, error) {
	changed, err := p.run(ctx, "diff", "--name-only", "HEAD")
	if err != nil {
		return Snapshot{}, err
	}
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
	return Snapshot{ChangedFiles: lines(changed), Diff: diff, RecentCommits: lines(logText)}, nil
}
