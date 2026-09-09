package projectmode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const baselineVersion = "brownfield-baseline-v1"

type Project struct {
	Mode         string    `json:"mode"`
	Root         string    `json:"root"`
	Git          bool      `json:"git"`
	Head         string    `json:"head,omitempty"`
	Branch       string    `json:"branch,omitempty"`
	Languages    []string  `json:"languages,omitempty"`
	Frameworks   []string  `json:"frameworks,omitempty"`
	BuildTools   []string  `json:"build_tools,omitempty"`
	TestTools    []string  `json:"test_tools,omitempty"`
	CI           []string  `json:"ci,omitempty"`
	ConfigFiles  []string  `json:"config_files,omitempty"`
	TrackedFiles int       `json:"tracked_files"`
	DirtyFiles   []string  `json:"dirty_files,omitempty"`
	DetectedAt   time.Time `json:"detected_at"`
}

type Baseline struct {
	Version         string            `json:"version"`
	SessionID       string            `json:"session_id"`
	Task            string            `json:"task"`
	TaskKind        string            `json:"task_kind"`
	Project         Project           `json:"project"`
	BaseHead        string            `json:"base_head,omitempty"`
	UserDirty       []string          `json:"user_dirty,omitempty"`
	UserDirtyHashes map[string]string `json:"user_dirty_hashes,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
}

type Ownership struct {
	BaseHead   string   `json:"base_head,omitempty"`
	UserDirty  []string `json:"user_dirty,omitempty"`
	BrainDelta []string `json:"brain_delta,omitempty"`
	Conflicts  []string `json:"ownership_conflicts,omitempty"`
}

type Impact struct {
	SessionID        string    `json:"session_id"`
	Task             string    `json:"task"`
	TaskKind         string    `json:"task_kind"`
	ProjectMode      string    `json:"project_mode"`
	AffectedFiles    []string  `json:"affected_files,omitempty"`
	RelatedTests     []string  `json:"related_tests,omitempty"`
	RelatedConfig    []string  `json:"related_config,omitempty"`
	RecentHistory    []string  `json:"recent_history,omitempty"`
	CurrentDiff      string    `json:"current_diff,omitempty"`
	Ownership        Ownership `json:"ownership"`
	RegressionWindow []string  `json:"regression_window,omitempty"`
	DiscoveryReady   bool      `json:"discovery_ready"`
	Missing          []string  `json:"missing,omitempty"`
}

type Engine struct{ Root, StateDir string }

func (e Engine) git(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", e.Root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func splitLines(raw string) []string {
	var out []string
	for _, s := range strings.Split(raw, "\n") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (e Engine) Detect(ctx context.Context) (Project, error) {
	p := Project{Root: e.Root, DetectedAt: time.Now().UTC()}
	if _, err := e.git(ctx, "rev-parse", "--is-inside-work-tree"); err == nil {
		p.Git = true
		p.Head, _ = e.git(ctx, "rev-parse", "HEAD")
		p.Branch, _ = e.git(ctx, "branch", "--show-current")
		tracked, _ := e.git(ctx, "ls-files")
		p.TrackedFiles = len(splitLines(tracked))
		dirty, _ := e.git(ctx, "status", "--porcelain=v1", "--untracked-files=all")
		p.DirtyFiles = porcelainFiles(dirty)
	}
	files, _ := walkNames(e.Root, 4, 5000)
	p.Languages, p.Frameworks, p.BuildTools, p.TestTools, p.CI, p.ConfigFiles = classifyProject(files)
	if p.Git && p.Head != "" && p.TrackedFiles > 0 {
		p.Mode = "existing"
	} else {
		p.Mode = "new"
	}
	return p, nil
}

func normalizeTask(task string) string { return strings.Join(strings.Fields(strings.TrimSpace(task)), " ") }

func (e Engine) Begin(ctx context.Context, sessionID, task string) (Baseline, error) {
	if strings.TrimSpace(sessionID) == "" {
		return Baseline{}, fmt.Errorf("session_id is required")
	}
	if existing, err := e.loadBaseline(sessionID); err == nil {
		if normalizeTask(existing.Task) != normalizeTask(task) {
			return Baseline{}, fmt.Errorf("brownfield session %s already belongs to a different task; start a new session to preserve the original baseline", sessionID)
		}
		return existing, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Baseline{}, err
	}

	project, err := e.Detect(ctx)
	if err != nil {
		return Baseline{}, err
	}
	b := Baseline{Version: baselineVersion, SessionID: sessionID, Task: task, TaskKind: TaskKind(task), Project: project, BaseHead: project.Head, UserDirty: append([]string(nil), project.DirtyFiles...), UserDirtyHashes: map[string]string{}, CreatedAt: time.Now().UTC()}
	for _, path := range b.UserDirty {
		b.UserDirtyHashes[path] = fileHash(filepath.Join(e.Root, path))
	}
	if err := e.saveBaseline(b); err != nil {
		return Baseline{}, err
	}
	return b, nil
}

func (e Engine) Impact(ctx context.Context, sessionID string) (Impact, error) {
	b, err := e.loadBaseline(sessionID)
	if err != nil {
		return Impact{}, err
	}
	current, err := e.Detect(ctx)
	if err != nil {
		return Impact{}, err
	}
	tracked, _ := e.git(ctx, "ls-files")
	allFiles := splitLines(tracked)
	untracked, _ := e.git(ctx, "ls-files", "--others", "--exclude-standard")
	allFiles = append(allFiles, splitLines(untracked)...)
	affected := rankFiles(b.Task, allFiles, current.DirtyFiles, 24)
	tests, configs := relatedFiles(affected, allFiles)
	diff, _ := e.git(ctx, "diff", "--unified=1", "HEAD", "--", ".", ":(exclude).project-brain")
	if len(diff) > 12000 {
		diff = diff[:12000] + "\n...diff truncated..."
	}
	history := e.history(ctx, affected, 16)
	regression := []string{}
	if b.TaskKind == "bugfix" {
		regression = e.regressionWindow(ctx, affected, 12)
	}
	ownership := computeOwnership(e.Root, b, current.DirtyFiles)
	missing := []string{}
	if current.Mode == "existing" {
		if len(affected) == 0 {
			missing = append(missing, "affected_files")
		}
		if b.BaseHead == "" {
			missing = append(missing, "base_head")
		}
		if len(allFiles) == 0 {
			missing = append(missing, "repository_files")
		}
	}
	ready := current.Mode != "existing" || len(missing) == 0
	return Impact{SessionID: sessionID, Task: b.Task, TaskKind: b.TaskKind, ProjectMode: current.Mode, AffectedFiles: affected, RelatedTests: tests, RelatedConfig: configs, RecentHistory: history, CurrentDiff: diff, Ownership: ownership, RegressionWindow: regression, DiscoveryReady: ready, Missing: missing}, nil
}

func (e Engine) baselinePath(sessionID string) string {
	return filepath.Join(e.StateDir, "brownfield", safeID(sessionID)+".json")
}
func (e Engine) saveBaseline(b Baseline) error {
	path := e.baselinePath(b.SessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(b, "", "  ")
	return os.WriteFile(path, data, 0o644)
}
func (e Engine) loadBaseline(id string) (Baseline, error) {
	data, err := os.ReadFile(e.baselinePath(id))
	if err != nil {
		return Baseline{}, err
	}
	var b Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return Baseline{}, err
	}
	return b, nil
}

func TaskKind(task string) string {
	t := strings.ToLower(task)
	switch {
	case contains(t, "bug", "fix", "broken", "regression", "ошиб", "баг", "почин", "фикс"):
		return "bugfix"
	case contains(t, "feature", "add ", "implement", "extend", "добав", "доработ"):
		return "feature-extension"
	case contains(t, "refactor", "rename", "cleanup", "рефактор"):
		return "refactor"
	case contains(t, "test", "coverage", "тест"):
		return "tests"
	case contains(t, "docs", "readme", "documentation", "документац"):
		return "docs"
	default:
		return "implementation"
	}
}

func computeOwnership(root string, b Baseline, current []string) Ownership {
	brain, conflicts := []string{}, []string{}
	for _, f := range current {
		h := fileHash(filepath.Join(root, f))
		old, wasUser := b.UserDirtyHashes[f]
		if !wasUser {
			brain = append(brain, f)
			continue
		}
		if h != old {
			brain = append(brain, f)
			conflicts = append(conflicts, f)
		}
	}
	return Ownership{BaseHead: b.BaseHead, UserDirty: b.UserDirty, BrainDelta: uniqueSorted(brain), Conflicts: uniqueSorted(conflicts)}
}

func (e Engine) history(ctx context.Context, files []string, n int) []string {
	if len(files) == 0 {
		return nil
	}
	args := []string{"log", "--oneline", "-n", fmt.Sprint(n), "--"}
	args = append(args, files...)
	raw, _ := e.git(ctx, args...)
	return splitLines(raw)
}
func (e Engine) regressionWindow(ctx context.Context, files []string, n int) []string {
	if len(files) == 0 {
		return nil
	}
	args := []string{"log", "--format=%h %ad %s", "--date=short", "-n", fmt.Sprint(n), "--"}
	args = append(args, files...)
	raw, _ := e.git(ctx, args...)
	return splitLines(raw)
}

func porcelainFiles(raw string) []string {
	var out []string
	for _, line := range splitLines(raw) {
		if len(line) < 4 {
			continue
		}
		p := strings.TrimSpace(line[3:])
		if i := strings.LastIndex(p, " -> "); i >= 0 {
			p = p[i+4:]
		}
		p = strings.Trim(p, `"`)
		if p != "" && !strings.HasPrefix(filepath.ToSlash(p), ".project-brain/") {
			out = append(out, p)
		}
	}
	return uniqueSorted(out)
}
func safeID(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:12]) }
func fileHash(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "missing"
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
func contains(s string, values ...string) bool {
	for _, v := range values {
		if strings.Contains(s, v) {
			return true
		}
	}
	return false
}
func uniqueSorted(in []string) []string {
	m := map[string]bool{}
	for _, v := range in {
		if v != "" {
			m[v] = true
		}
	}
	out := make([]string, 0, len(m))
	for v := range m {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func walkNames(root string, maxDepth, maxFiles int) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			base := d.Name()
			if base == ".git" || base == ".project-brain" || base == "node_modules" || base == "vendor" || base == "dist" || base == "build" {
				return filepath.SkipDir
			}
			if rel != "." && strings.Count(filepath.ToSlash(rel), "/") >= maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if len(out) >= maxFiles {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}

func classifyProject(files []string) (langs, frameworks, builds, tests, ci, configs []string) {
	m := map[string]bool{}
	for _, f := range files {
		m[strings.ToLower(filepath.Base(f))] = true
		ext := strings.ToLower(filepath.Ext(f))
		switch ext {
		case ".go":
			langs = append(langs, "Go")
		case ".py":
			langs = append(langs, "Python")
		case ".ts":
			langs = append(langs, "TypeScript")
		case ".js":
			langs = append(langs, "JavaScript")
		case ".rs":
			langs = append(langs, "Rust")
		case ".java":
			langs = append(langs, "Java")
		case ".kt":
			langs = append(langs, "Kotlin")
		case ".cs":
			langs = append(langs, "C#")
		}
		lower := strings.ToLower(f)
		if strings.Contains(lower, ".github/workflows/") {
			ci = append(ci, "GitHub Actions")
		}
		if strings.Contains(lower, "test") || strings.Contains(lower, "spec") {
			tests = append(tests, "repository tests")
		}
	}
	if m["go.mod"] { builds = append(builds, "Go modules") }
	if m["package.json"] { builds = append(builds, "npm/node"); configs = append(configs, "package.json") }
	if m["pyproject.toml"] { builds = append(builds, "pyproject") }
	if m["cargo.toml"] { builds = append(builds, "Cargo") }
	if m["pom.xml"] { builds = append(builds, "Maven") }
	if m["gradlew"] || m["build.gradle"] || m["build.gradle.kts"] { builds = append(builds, "Gradle") }
	if m["angular.json"] { frameworks = append(frameworks, "Angular") }
	if m["next.config.js"] || m["next.config.mjs"] || m["next.config.ts"] { frameworks = append(frameworks, "Next.js") }
	if m["vite.config.ts"] || m["vite.config.js"] { frameworks = append(frameworks, "Vite") }
	for _, name := range []string{"dockerfile", "docker-compose.yml", "compose.yml", ".env.example", "tsconfig.json", "pytest.ini", "jest.config.js"} {
		if m[name] { configs = append(configs, name) }
	}
	return uniqueSorted(langs), uniqueSorted(frameworks), uniqueSorted(builds), uniqueSorted(tests), uniqueSorted(ci), uniqueSorted(configs)
}

func taskTerms(task string) []string {
	stop := map[string]bool{"the": true, "and": true, "for": true, "with": true, "add": true, "fix": true, "bug": true, "this": true, "that": true, "нужно": true, "сделать": true, "добавить": true, "починить": true}
	var out []string
	for _, x := range strings.FieldsFunc(strings.ToLower(task), func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r >= 'а' && r <= 'я') }) {
		if len([]rune(x)) >= 3 && !stop[x] { out = append(out, x) }
	}
	return uniqueSorted(out)
}
func rankFiles(task string, files, changed []string, limit int) []string {
	terms := taskTerms(task)
	changedM := map[string]bool{}
	for _, f := range changed { changedM[f] = true }
	type scored struct{ p string; s int }
	var rows []scored
	for _, f := range uniqueSorted(files) {
		lower := strings.ToLower(f); s := 0
		if changedM[f] { s += 8 }
		for _, t := range terms { if strings.Contains(lower, t) { s += 4 } }
		if strings.Contains(lower, "test") || strings.Contains(lower, "spec") { s-- }
		if s > 0 { rows = append(rows, scored{f, s}) }
	}
	sort.Slice(rows, func(i, j int) bool { if rows[i].s == rows[j].s { return rows[i].p < rows[j].p }; return rows[i].s > rows[j].s })
	if len(rows) > limit { rows = rows[:limit] }
	out := make([]string, len(rows)); for i, r := range rows { out[i] = r.p }; return out
}
func relatedFiles(affected, all []string) (tests, configs []string) {
	stems := []string{}
	for _, f := range affected { base := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)); if len(base) > 2 { stems = append(stems, strings.ToLower(base)) } }
	for _, f := range all {
		l := strings.ToLower(f)
		for _, s := range stems {
			if !strings.Contains(l, s) { continue }
			if strings.Contains(l, "test") || strings.Contains(l, "spec") { tests = append(tests, f) }
			if strings.Contains(l, "config") || strings.HasSuffix(l, ".json") || strings.HasSuffix(l, ".yaml") || strings.HasSuffix(l, ".yml") || strings.HasSuffix(l, ".toml") { configs = append(configs, f) }
		}
	}
	return uniqueSorted(tests), uniqueSorted(configs)
}
