package autotune

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ulaista/usage-ai-on-dev/internal/economy"
	"github.com/ulaista/usage-ai-on-dev/internal/hardware"
)

const benchmarkCacheVersion = "autotune-v4"

type CachedReport struct {
	Report Report `json:"report"`
	Key    string `json:"key"`
	Hit    bool   `json:"cache_hit"`
}

func (r *Runner) RunCached(ctx context.Context, opts Options, force bool) (CachedReport, error) {
	r.init()
	snapshot, err := r.Detector.Snapshot(ctx)
	if err != nil { return CachedReport{}, err }
	policy, err := hardware.LoadPolicy(hardware.PolicyPath(r.StateDir))
	if err != nil { return CachedReport{}, err }
	view := hardware.BuildView(snapshot, policy)
	models, err := r.listModels(ctx)
	if err != nil { return CachedReport{}, err }
	models = filterModels(models, opts.Models, opts.MaxModels)
	modelJSON, _ := json.Marshal(models)
	inventoryDigest, err := r.ollamaInventoryDigest(ctx)
	if err != nil { return CachedReport{}, err }
	policyJSON, _ := json.Marshal(view.Effective)
	contexts := append([]int(nil), opts.Contexts...)
	sort.Ints(contexts)
	contextJSON, _ := json.Marshal(contexts)
	key := economy.Fingerprint(benchmarkCacheVersion, snapshot.Inventory.HardwareID, string(policyJSON), inventoryDigest, string(modelJSON), string(contextJSON), strconv.Itoa(opts.MaxModels), strconv.Itoa(opts.OutputTokens))
	cache := economy.FileCache{Dir: filepath.Join(r.StateDir, "cache")}
	if !force {
		var report Report
		if hit, loadErr := cache.Load("autotune", key, &report); loadErr == nil && hit { return CachedReport{Report: report, Key: key, Hit: true}, nil }
	}
	report, err := r.Run(ctx, opts)
	if err != nil { return CachedReport{}, err }
	_ = cache.Save("autotune", key, report)
	return CachedReport{Report: report, Key: key}, nil
}

func (r *Runner) ollamaInventoryDigest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(r.OllamaURL, "/")+"/api/tags", nil)
	if err != nil { return "", err }
	resp, err := r.HTTPClient.Do(req)
	if err != nil { return "", fmt.Errorf("ollama inventory fingerprint failed: %w", err) }
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { return "", fmt.Errorf("ollama inventory fingerprint returned %s", resp.Status) }
	return economy.Fingerprint(string(body)), nil
}

func ReportTokenCost(report Report) (input, output int) {
	for _, model := range report.Models { for _, trial := range model.Trials { input += trial.PromptTokens; output += trial.OutputTokens } }
	return input, output
}
