package autotune

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func ReportPath(stateDir string) string {
	return filepath.Join(stateDir, "autotune-report.json")
}

func SaveReport(path string, report Report) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func LoadReport(path string) (Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return Report{}, err
	}
	return report, nil
}

func ValidateReportForHardware(report Report, currentHardwareID string) error {
	if report.Hardware.Snapshot.Inventory.HardwareID == "" {
		return fmt.Errorf("benchmark report has no hardware ID")
	}
	if report.Hardware.Snapshot.Inventory.HardwareID != currentHardwareID {
		return fmt.Errorf("benchmark was generated for different hardware")
	}
	if report.Recommendation.PreferredModel == "" {
		return fmt.Errorf("benchmark report has no usable recommendation")
	}
	return nil
}
