package autotune

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/ulaista/usage-ai-on-dev/internal/economy"
	"github.com/ulaista/usage-ai-on-dev/internal/hardware"
)

const benchmarkCacheVersion = "autotune-v3"

type CachedReport struct {
	Report Report `json:"report"`
	Key    string `json:"key"`
	Hit    bool   `json:"cache_hit"`
}

func (r *Runner) RunCached(ctx context.Context, opts Options, force bool) (CachedReport, error) {
	r.init()
	snapshot, err := r.Detector.Snapshot(ctx)
	if err != nil {
		return CachedReport{}, err
	}
	policy, err := hardware.LoadPolicy(hardware.PolicyPath(r.StateDir))
	if err != nil {
		return CachedReport{}, err
	}
	view := hardware.BuildView(snapshot, policy)
	models, err := r.listModels(ctx)
	if err != nil {
		return CachedReport{}, err
	}
	models = filterModels(models, opts.Models, opts.MaxModels)
	modelJSON, _ := json.Marshal(models)
	policyJSON, _ := json.Marshal(view.Effective)
	contexts := append([]int(nil), opts.Contexts...)
	sort.Ints(contexts)
	contextJSON, _ := json.Marshal(contexts)
	key := economy.Fingerprint(benchmarkCacheVersion, snapshot.Inventory.HardwareID, string(policyJSON), string(modelJSON), string(contextJSON), strconv.Itoa(opts.MaxModels), strconv.Itoa(opts.OutputTokens))
	cache := economy.FileCache{Dir: filepath.Join(r.StateDir, "cache")}
	if !force {
		var report Report
		if hit, loadErr := cache.Load("autotune", key, &report); loadErr == nil && hit {
			return CachedReport{Report: report, Key: key, Hit: true}, nil
		}
	}
	report, err := r.Run(ctx, opts)
	if err != nil {
		return CachedReport{}, err
	}
	_ = cache.Save("autotune", key, report)
	return CachedReport{Report: report, Key: key}, nil
}

func ReportTokenCost(report Report) (input, output int) {
	for _, model := range report.Models {
		for _, trial := range model.Trials {
			input += trial.PromptTokens
			output += trial.OutputTokens
		}
	}
	return input, output
}
