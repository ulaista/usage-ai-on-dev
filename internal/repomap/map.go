package repomap

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ulaista/usage-ai-on-dev/internal/domain"
)

const cacheVersion = 1

var (
	termRE       = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_-]{2,}`)
	pythonImport = regexp.MustCompile(`(?m)^\s*(?:from\s+([A-Za-z0-9_.]+)\s+import|import\s+([A-Za-z0-9_.]+))`)
	pythonSymbol = regexp.MustCompile(`(?m)^\s*(?:async\s+)?(def|class)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	jsImport     = regexp.MustCompile(`(?m)(?:import|export)[^\n]*?from\s*["']([^"']+)["']|require\(\s*["']([^"']+)["']\s*\)`)
	jsSymbol     = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:default\s+)?(class|function|interface|type|enum|const)\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
)

var codeExtensions = map[string]bool{
	".go": true, ".py": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true,
	".java": true, ".kt": true, ".rs": true, ".cs": true, ".rb": true, ".php": true,
	".swift": true, ".vue": true, ".svelte": true, ".sql": true,
}

type Entry struct {
	Path            string   `json:"path"`
	Score           float64  `json:"score"`
	EstimatedTokens int      `json:"estimated_tokens"`
	Signatures      []string `json:"signatures,omitempty"`
	Dependencies    []string `json:"dependencies,omitempty"`
	Reasons         []string `json:"reasons,omitempty"`
}

type Map struct {
	Entries         []Entry `json:"entries"`
	EstimatedTokens int     `json:"estimated_tokens"`
	ScannedFiles    int     `json:"scanned_files"`
	AnalyzedFiles   int     `json:"analyzed_files"`
	ReusedFiles     int     `json:"reused_files"`
	Edges           int     `json:"edges"`
}

type Request struct {
	Task         string
	ChangedFiles []string
	Semantic     []domain.SemanticResult
	TokenBudget  int
}

type Builder struct {
	Root         string
	CachePath    string
	MaxFiles     int
	MaxFileBytes int64
}

type fileRef struct {
	Path string
	Blob string
}

type node struct {
	Path            string   `json:"path"`
	Signatures      []string `json:"signatures,omitempty"`
	RawDependencies []string `json:"raw_dependencies,omitempty"`
}

type cachedNode struct {
	Blob string `json:"blob"`
	Node node   `json:"node"`
}

type cacheFile struct {
	Version int                   `json:"version"`
	Files   map[string]cachedNode `json:"files"`
}

func (b Builder) Build(ctx context.Context, req Request) (Map, error) {
	if req.TokenBudget <= 0 {
		req.TokenBudget = 4000
	}
	if b.MaxFiles <= 0 {
		b.MaxFiles = 8000
	}
	if b.MaxFileBytes <= 0 {
		b.MaxFileBytes = 512 * 1024
	}
	if b.CachePath == "" {
		b.CachePath = filepath.Join(b.Root, ".project-brain", "cache", "repomap.json")
	}

	refs, err := b.fileRefs(ctx)
	if err != nil {
		return Map{}, err
	}
	if len(refs) > b.MaxFiles {
		refs = refs[:b.MaxFiles]
	}
	changed := stringSet(req.ChangedFiles)
	cache := b.loadCache()
	freshCache := cacheFile{Version: cacheVersion, Files: make(map[string]cachedNode, len(refs))}
	nodes := make(map[string]node, len(refs))
	result := Map{ScannedFiles: len(refs)}

	for _, ref := range refs {
		cached, ok := cache.Files[ref.Path]
		if ok && cached.Blob == ref.Blob && !changed[ref.Path] && ref.Blob != "worktree" {
			nodes[ref.Path] = cached.Node
			freshCache.Files[ref.Path] = cached
			result.ReusedFiles++
			continue
		}
		n, err := b.analyze(ref.Path)
		if err != nil {
			continue
		}
		nodes[ref.Path] = n
		freshCache.Files[ref.Path] = cachedNode{Blob: ref.Blob, Node: n}
		result.AnalyzedFiles++
	}
	_ = b.saveCache(freshCache)

	modulePath := b.goModulePath()
	adjacency := make(map[string]map[string]struct{}, len(nodes))
	for path := range nodes {
		adjacency[path] = map[string]struct{}{}
	}
	for source, n := range nodes {
		for _, raw := range n.RawDependencies {
			for _, target := range resolveDependency(source, raw, modulePath, nodes) {
				if target == source {
					continue
				}
				if _, ok := adjacency[source][target]; !ok {
					result.Edges++
				}
				adjacency[source][target] = struct{}{}
				adjacency[target][source] = struct{}{}
			}
		}
	}

	base, reasons := seedScores(req, nodes, changed)
	scores := rank(base, adjacency)
	type ranked struct {
		path  string
		score float64
	}
	rankedNodes := make([]ranked, 0, len(nodes))
	for path := range nodes {
		rankedNodes = append(rankedNodes, ranked{path: path, score: scores[path]})
	}
	sort.Slice(rankedNodes, func(i, j int) bool {
		if rankedNodes[i].score == rankedNodes[j].score {
			return rankedNodes[i].path < rankedNodes[j].path
		}
		return rankedNodes[i].score > rankedNodes[j].score
	})

	used := 0
	for _, item := range rankedNodes {
		n := nodes[item.path]
		deps := sortedKeys(adjacency[item.path], 8)
		entry := Entry{
			Path:         item.path,
			Score:        math.Round(item.score*1000) / 1000,
			Signatures:   limitStrings(n.Signatures, 12),
			Dependencies: deps,
			Reasons:      sortedKeys(reasons[item.path], 8),
		}
		entry.EstimatedTokens = estimateEntryTokens(entry)
		if used+entry.EstimatedTokens > req.TokenBudget {
			continue
		}
		result.Entries = append(result.Entries, entry)
		used += entry.EstimatedTokens
		if len(result.Entries) >= 60 {
			break
		}
	}
	result.EstimatedTokens = used
	return result, nil
}

func (b Builder) fileRefs(ctx context.Context) ([]fileRef, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", b.Root, "ls-files", "-s", "-z")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("repo map git ls-files: %w", err)
	}
	seen := map[string]bool{}
	var refs []fileRef
	for _, record := range strings.Split(string(out), "\x00") {
		if record == "" {
			continue
		}
		parts := strings.SplitN(record, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		meta := strings.Fields(parts[0])
		if len(meta) < 2 || !isCodePath(parts[1]) {
			continue
		}
		refs = append(refs, fileRef{Path: filepath.ToSlash(parts[1]), Blob: meta[1]})
		seen[filepath.ToSlash(parts[1])] = true
	}

	cmd = exec.CommandContext(ctx, "git", "-C", b.Root, "ls-files", "--others", "--exclude-standard", "-z")
	out, err = cmd.Output()
	if err == nil {
		for _, path := range strings.Split(string(out), "\x00") {
			path = filepath.ToSlash(path)
			if path == "" || seen[path] || !isCodePath(path) {
				continue
			}
			refs = append(refs, fileRef{Path: path, Blob: "worktree"})
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Path < refs[j].Path })
	return refs, nil
}

func isCodePath(path string) bool {
	clean := filepath.ToSlash(path)
	if strings.Contains(clean, "/node_modules/") || strings.Contains(clean, "/vendor/") || strings.HasPrefix(clean, ".project-brain/") {
		return false
	}
	return codeExtensions[strings.ToLower(filepath.Ext(clean))]
}

func (b Builder) analyze(relative string) (node, error) {
	full := filepath.Join(b.Root, filepath.FromSlash(relative))
	info, err := os.Stat(full)
	if err != nil {
		return node{}, err
	}
	if info.Size() > b.MaxFileBytes {
		return node{}, fmt.Errorf("file too large")
	}
	content, err := os.ReadFile(full)
	if err != nil {
		return node{}, err
	}
	n := node{Path: relative}
	switch strings.ToLower(filepath.Ext(relative)) {
	case ".go":
		n.Signatures, n.RawDependencies = analyzeGo(relative, content)
	case ".py":
		n.Signatures, n.RawDependencies = analyzePython(string(content))
	case ".js", ".jsx", ".ts", ".tsx", ".vue", ".svelte":
		n.Signatures, n.RawDependencies = analyzeJS(string(content))
	default:
		n.Signatures = genericSignatures(string(content))
	}
	return n, nil
}

func analyzeGo(filename string, content []byte) ([]string, []string) {
	file, err := parser.ParseFile(token.NewFileSet(), filename, content, parser.SkipObjectResolution)
	if err != nil {
		return nil, nil
	}
	var signatures []string
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			kind := "func"
			if d.Recv != nil {
				kind = "method"
			}
			signatures = append(signatures, kind+" "+d.Name.Name)
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					signatures = append(signatures, "type "+ts.Name.Name)
				}
			}
		}
	}
	var deps []string
	for _, item := range file.Imports {
		if value, err := strconv.Unquote(item.Path.Value); err == nil {
			deps = append(deps, value)
		}
	}
	return uniqueSorted(signatures), uniqueSorted(deps)
}

func analyzePython(content string) ([]string, []string) {
	var signatures, deps []string
	for _, match := range pythonSymbol.FindAllStringSubmatch(content, -1) {
		signatures = append(signatures, match[1]+" "+match[2])
	}
	for _, match := range pythonImport.FindAllStringSubmatch(content, -1) {
		dep := match[1]
		if dep == "" {
			dep = match[2]
		}
		if dep != "" {
			deps = append(deps, dep)
		}
	}
	return uniqueSorted(signatures), uniqueSorted(deps)
}

func analyzeJS(content string) ([]string, []string) {
	var signatures, deps []string
	for _, match := range jsSymbol.FindAllStringSubmatch(content, -1) {
		signatures = append(signatures, match[1]+" "+match[2])
	}
	for _, match := range jsImport.FindAllStringSubmatch(content, -1) {
		dep := match[1]
		if dep == "" {
			dep = match[2]
		}
		if dep != "" {
			deps = append(deps, dep)
		}
	}
	return uniqueSorted(signatures), uniqueSorted(deps)
}

func genericSignatures(content string) []string {
	matches := jsSymbol.FindAllStringSubmatch(content, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, match[1]+" "+match[2])
	}
	return uniqueSorted(out)
}

func (b Builder) goModulePath() string {
	data, err := os.ReadFile(filepath.Join(b.Root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

func resolveDependency(source, raw, modulePath string, nodes map[string]node) []string {
	ext := strings.ToLower(filepath.Ext(source))
	switch ext {
	case ".js", ".jsx", ".ts", ".tsx", ".vue", ".svelte":
		if strings.HasPrefix(raw, ".") {
			return resolveRelative(source, raw, nodes)
		}
	case ".py":
		return resolvePython(source, raw, nodes)
	case ".go":
		if modulePath != "" && (raw == modulePath || strings.HasPrefix(raw, modulePath+"/")) {
			dir := strings.TrimPrefix(strings.TrimPrefix(raw, modulePath), "/")
			return filesInDir(dir, ".go", nodes)
		}
	}
	return nil
}

func resolveRelative(source, raw string, nodes map[string]node) []string {
	base := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(source), filepath.FromSlash(raw))))
	candidates := []string{base}
	for _, ext := range []string{".ts", ".tsx", ".js", ".jsx", ".vue", ".svelte"} {
		candidates = append(candidates, base+ext, base+"/index"+ext)
	}
	for _, candidate := range candidates {
		candidate = filepath.ToSlash(candidate)
		if _, ok := nodes[candidate]; ok {
			return []string{candidate}
		}
	}
	return nil
}

func resolvePython(source, raw string, nodes map[string]node) []string {
	dots := 0
	for dots < len(raw) && raw[dots] == '.' {
		dots++
	}
	module := strings.TrimLeft(raw, ".")
	baseDir := filepath.ToSlash(filepath.Dir(source))
	for i := 1; i < dots; i++ {
		baseDir = filepath.ToSlash(filepath.Dir(baseDir))
	}
	modulePath := strings.ReplaceAll(module, ".", "/")
	var base string
	if dots > 0 {
		base = strings.Trim(strings.Join([]string{baseDir, modulePath}, "/"), "/")
	} else {
		base = modulePath
	}
	for _, candidate := range []string{base + ".py", base + "/__init__.py"} {
		candidate = strings.TrimPrefix(candidate, "./")
		if _, ok := nodes[candidate]; ok {
			return []string{candidate}
		}
	}
	return nil
}

func filesInDir(dir, ext string, nodes map[string]node) []string {
	dir = strings.Trim(dir, "/")
	var out []string
	for path := range nodes {
		if filepath.ToSlash(filepath.Dir(path)) == dir && strings.EqualFold(filepath.Ext(path), ext) {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

func seedScores(req Request, nodes map[string]node, changed map[string]bool) (map[string]float64, map[string]map[string]struct{}) {
	terms := queryTerms(req.Task)
	base := make(map[string]float64, len(nodes))
	reasons := make(map[string]map[string]struct{}, len(nodes))
	for path, n := range nodes {
		base[path] = 0.05
		reasons[path] = map[string]struct{}{}
		lowerPath := strings.ToLower(path)
		if changed[path] {
			base[path] += 12
			reasons[path]["changed"] = struct{}{}
		}
		for _, semantic := range req.Semantic {
			if strings.Contains(semantic.Raw, path) {
				base[path] += 8
				reasons[path]["semantic"] = struct{}{}
				break
			}
		}
		joinedSignatures := strings.ToLower(strings.Join(n.Signatures, " "))
		for _, term := range terms {
			if strings.Contains(lowerPath, term) {
				base[path] += 3
				reasons[path]["task:"+term] = struct{}{}
			}
			if strings.Contains(joinedSignatures, term) {
				base[path] += 1.5
				reasons[path]["symbol:"+term] = struct{}{}
			}
		}
	}
	return base, reasons
}

func rank(base map[string]float64, adjacency map[string]map[string]struct{}) map[string]float64 {
	scores := make(map[string]float64, len(base))
	for path, value := range base {
		scores[path] = value
	}
	for iteration := 0; iteration < 7; iteration++ {
		next := make(map[string]float64, len(base))
		for path, seed := range base {
			next[path] = 0.60*seed + 0.08*math.Log1p(float64(len(adjacency[path])))
		}
		for source, neighbors := range adjacency {
			if len(neighbors) == 0 {
				continue
			}
			share := 0.32 * scores[source] / float64(len(neighbors))
			for target := range neighbors {
				next[target] += share
			}
		}
		scores = next
	}
	return scores
}

func queryTerms(task string) []string {
	stop := map[string]bool{"the": true, "and": true, "for": true, "with": true, "from": true, "into": true, "this": true, "that": true, "update": true, "implement": true}
	seen := map[string]bool{}
	var out []string
	for _, raw := range termRE.FindAllString(task, -1) {
		term := strings.ToLower(raw)
		if stop[term] || seen[term] {
			continue
		}
		seen[term] = true
		out = append(out, term)
		if len(out) >= 16 {
			break
		}
	}
	return out
}

func estimateEntryTokens(entry Entry) int {
	data, _ := json.Marshal(entry)
	return max(1, len(data)/4)
}

func (m Map) RenderMarkdown() string {
	if len(m.Entries) == 0 {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Repo-map: %d files selected from %d, %d dependency edges, ~%d tokens\n", len(m.Entries), m.ScannedFiles, m.Edges, m.EstimatedTokens)
	for _, entry := range m.Entries {
		fmt.Fprintf(&out, "- %s [score %.3f", entry.Path, entry.Score)
		if len(entry.Reasons) > 0 {
			fmt.Fprintf(&out, "; %s", strings.Join(entry.Reasons, ", "))
		}
		out.WriteString("]\n")
		if len(entry.Signatures) > 0 {
			fmt.Fprintf(&out, "  symbols: %s\n", strings.Join(entry.Signatures, ", "))
		}
		if len(entry.Dependencies) > 0 {
			fmt.Fprintf(&out, "  related: %s\n", strings.Join(entry.Dependencies, ", "))
		}
	}
	return out.String()
}

func (b Builder) loadCache() cacheFile {
	data, err := os.ReadFile(b.CachePath)
	if err != nil {
		return cacheFile{Version: cacheVersion, Files: map[string]cachedNode{}}
	}
	var cache cacheFile
	if json.Unmarshal(data, &cache) != nil || cache.Version != cacheVersion || cache.Files == nil {
		return cacheFile{Version: cacheVersion, Files: map[string]cachedNode{}}
	}
	return cache
}

func (b Builder) saveCache(cache cacheFile) error {
	if err := os.MkdirAll(filepath.Dir(b.CachePath), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	return os.WriteFile(b.CachePath, data, 0o644)
}

func uniqueSorted(items []string) []string {
	set := map[string]struct{}{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			set[item] = struct{}{}
		}
	}
	return sortedKeys(set, 0)
}

func stringSet(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		out[filepath.ToSlash(item)] = true
	}
	return out
}

func sortedKeys(values map[string]struct{}, limit int) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return limitStrings(out, limit)
}

func limitStrings(items []string, limit int) []string {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}
