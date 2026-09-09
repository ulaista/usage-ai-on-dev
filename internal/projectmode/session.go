package projectmode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// LatestSession returns the newest persisted brownfield session bound to the
// same normalized task. It lets guarded entry points recover an existing
// developer baseline without creating a new one and accidentally reclassifying
// in-progress changes as USER_DIRTY.
func LatestSession(stateDir, task string) (string, bool) {
	dir := filepath.Join(stateDir, "brownfield")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	wanted := normalizeTask(task)
	var best Baseline
	found := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var baseline Baseline
		if err := json.Unmarshal(data, &baseline); err != nil {
			continue
		}
		if baseline.Version != baselineVersion || normalizeTask(baseline.Task) != wanted || baseline.SessionID == "" {
			continue
		}
		if !found || baseline.CreatedAt.After(best.CreatedAt) {
			best = baseline
			found = true
		}
	}
	if !found {
		return "", false
	}
	return best.SessionID, true
}
