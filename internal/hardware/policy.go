package hardware

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Limits struct {
	MemoryReserveMB    int    `json:"memory_reserve_mb"`
	SoftContextTokens  int    `json:"soft_context_tokens"`
	HardContextTokens  int    `json:"hard_context_tokens"`
	MaxOutputTokens    int    `json:"max_output_tokens"`
	MaxParallelWorkers int    `json:"max_parallel_workers"`
	PreferredModel     string `json:"preferred_model,omitempty"`
	PreferredModelClass string `json:"preferred_model_class,omitempty"`
}

type Policy struct {
	Accepted        bool       `json:"accepted"`
	AcceptedAt      *time.Time `json:"accepted_at,omitempty"`
	HardwareID      string     `json:"hardware_id,omitempty"`
	Override        Limits     `json:"override,omitempty"`
}

type View struct {
	Snapshot               Snapshot `json:"snapshot"`
	Recommended            Limits   `json:"recommended"`
	Effective              Limits   `json:"effective"`
	Policy                 Policy   `json:"policy"`
	RequiresUserAcceptance bool     `json:"requires_user_acceptance"`
	HardwareChanged        bool     `json:"hardware_changed"`
}

func PolicyPath(stateDir string) string {
	return filepath.Join(stateDir, "hardware-policy.json")
}

func LoadPolicy(path string) (Policy, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Policy{}, nil
	}
	if err != nil {
		return Policy{}, err
	}
	var p Policy
	if err := json.Unmarshal(data, &p); err != nil {
		return Policy{}, err
	}
	return p, nil
}

func SavePolicy(path string, policy Policy) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func ResetPolicy(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func BuildView(snapshot Snapshot, policy Policy) View {
	recommended := LimitsFromProfile(snapshot.Profile)
	effective := recommended
	hardwareChanged := policy.HardwareID != "" && policy.HardwareID != snapshot.Inventory.HardwareID
	if policy.Accepted && !hardwareChanged {
		effective = ApplyOverride(recommended, policy.Override)
	}
	return View{
		Snapshot: snapshot, Recommended: recommended, Effective: effective, Policy: policy,
		RequiresUserAcceptance: !policy.Accepted || hardwareChanged,
		HardwareChanged: hardwareChanged,
	}
}

func AcceptPolicy(snapshot Snapshot, override Limits) Policy {
	now := time.Now().UTC()
	return Policy{Accepted: true, AcceptedAt: &now, HardwareID: snapshot.Inventory.HardwareID, Override: override}
}

func LimitsFromProfile(p Profile) Limits {
	return Limits{
		MemoryReserveMB: p.MemoryReserveMB,
		SoftContextTokens: p.SoftContextTokens,
		HardContextTokens: p.HardContextTokens,
		MaxOutputTokens: p.MaxOutputTokens,
		MaxParallelWorkers: p.MaxParallelWorkers,
		PreferredModelClass: p.PreferredModelClass,
	}
}

func ApplyOverride(base, override Limits) Limits {
	out := base
	if override.MemoryReserveMB > 0 { out.MemoryReserveMB = override.MemoryReserveMB }
	if override.SoftContextTokens > 0 { out.SoftContextTokens = override.SoftContextTokens }
	if override.HardContextTokens > 0 { out.HardContextTokens = override.HardContextTokens }
	if override.MaxOutputTokens > 0 { out.MaxOutputTokens = override.MaxOutputTokens }
	if override.MaxParallelWorkers > 0 { out.MaxParallelWorkers = override.MaxParallelWorkers }
	if override.PreferredModel != "" { out.PreferredModel = override.PreferredModel }
	if override.PreferredModelClass != "" { out.PreferredModelClass = override.PreferredModelClass }
	if out.HardContextTokens < out.SoftContextTokens { out.HardContextTokens = out.SoftContextTokens }
	return out
}
