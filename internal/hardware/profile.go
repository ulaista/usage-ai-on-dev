package hardware

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

type Profile struct {
	Name                string `json:"name"`
	Platform            string `json:"platform"`
	Chip                string `json:"chip"`
	TotalMemoryMB       int    `json:"total_memory_mb"`
	MemoryReserveMB     int    `json:"memory_reserve_mb"`
	SoftContextTokens   int    `json:"soft_context_tokens"`
	HardContextTokens   int    `json:"hard_context_tokens"`
	MaxOutputTokens     int    `json:"max_output_tokens"`
	MaxParallelWorkers  int    `json:"max_parallel_workers"`
	ThermalSensitive    bool   `json:"thermal_sensitive"`
	PreferredModelClass string `json:"preferred_model_class"`
}

type RuntimeResources struct {
	AvailableMemoryMB int    `json:"available_memory_mb"`
	SwapUsedMB        int    `json:"swap_used_mb"`
	MemoryPressure    string `json:"memory_pressure"`
}

type Snapshot struct {
	Profile   Profile          `json:"profile"`
	Resources RuntimeResources `json:"resources"`
}

type Detector interface {
	Snapshot(context.Context) (Snapshot, error)
}

type SystemDetector struct{}

func (SystemDetector) Snapshot(ctx context.Context) (Snapshot, error) {
	chip, totalMB := detectPlatform(ctx)
	profile := SelectProfile(runtime.GOOS, chip, totalMB)
	resources := detectRuntimeResources(ctx, totalMB)
	return Snapshot{Profile: profile, Resources: resources}, nil
}

func SelectProfile(platform, chip string, totalMB int) Profile {
	p := Profile{
		Name: "generic-lite", Platform: platform, Chip: chip, TotalMemoryMB: totalMB,
		MemoryReserveMB: 4096, SoftContextTokens: 6000, HardContextTokens: 12000,
		MaxOutputTokens: 1000, MaxParallelWorkers: 1, PreferredModelClass: "4b-q4",
	}
	lower := strings.ToLower(chip)
	if platform == "darwin" && strings.Contains(lower, "apple") {
		p.Name = "apple-lite"
		p.MemoryReserveMB = 5500
		p.ThermalSensitive = strings.Contains(lower, "m2") && !strings.Contains(lower, "pro") && !strings.Contains(lower, "max")
		if totalMB >= 17*1024 || strings.Contains(lower, "m3 pro") || strings.Contains(lower, "m4 pro") {
			p.Name = "apple-balanced"
			p.MemoryReserveMB = 5000
			p.SoftContextTokens = 8000
			p.HardContextTokens = 16000
			p.MaxOutputTokens = 1500
			p.PreferredModelClass = "4b-q4/8b-q4"
			p.ThermalSensitive = false
		}
	}
	return p
}

func (p Profile) Evaluate(r RuntimeResources, requestedContext int) Decision {
	if requestedContext <= 0 {
		requestedContext = p.SoftContextTokens
	}
	if requestedContext > p.HardContextTokens {
		return Decision{Allowed: false, RecommendedContextTokens: p.HardContextTokens, Reason: "requested context exceeds local hard limit"}
	}
	if r.AvailableMemoryMB <= 0 || r.MemoryPressure == "unknown" {
		return Decision{Allowed: true, RecommendedContextTokens: min(requestedContext, p.SoftContextTokens), Reason: "resource telemetry unavailable; using conservative profile limits"}
	}
	usable := r.AvailableMemoryMB - p.MemoryReserveMB
	if r.MemoryPressure == "critical" || usable < 1024 {
		return Decision{Allowed: false, RecommendedContextTokens: min(requestedContext, max(2048, p.SoftContextTokens/2)), Reason: "memory pressure is too high for safe local inference"}
	}
	if r.SwapUsedMB >= 2048 && r.AvailableMemoryMB < p.MemoryReserveMB+2048 {
		return Decision{Allowed: false, RecommendedContextTokens: min(requestedContext, max(2048, p.SoftContextTokens/2)), Reason: "swap usage and available memory indicate swap-storm risk"}
	}
	recommended := min(requestedContext, p.SoftContextTokens)
	if r.MemoryPressure == "warning" || usable < 3072 {
		recommended = min(recommended, max(2048, p.SoftContextTokens/2))
	}
	return Decision{Allowed: true, RecommendedContextTokens: recommended, Reason: "local execution fits current hardware budget"}
}

type Decision struct {
	Allowed                  bool   `json:"allowed"`
	RecommendedContextTokens int    `json:"recommended_context_tokens"`
	Reason                   string `json:"reason"`
}

func detectPlatform(ctx context.Context) (string, int) {
	if runtime.GOOS == "darwin" {
		chip := sysctl(ctx, "machdep.cpu.brand_string")
		if chip == "" {
			chip = sysctl(ctx, "hw.model")
		}
		total, _ := strconv.ParseInt(strings.TrimSpace(sysctl(ctx, "hw.memsize")), 10, 64)
		return chip, int(total / 1024 / 1024)
	}
	total, _, _ := linuxMemoryStats()
	return runtime.GOARCH, total
}

func detectRuntimeResources(ctx context.Context, totalMB int) RuntimeResources {
	if runtime.GOOS == "darwin" {
		available := darwinAvailableMemoryMB(ctx)
		swap := darwinSwapUsedMB(ctx)
		return RuntimeResources{AvailableMemoryMB: available, SwapUsedMB: swap, MemoryPressure: classifyPressure(totalMB, available, swap)}
	}
	_, available, swap := linuxMemoryStats()
	return RuntimeResources{AvailableMemoryMB: available, SwapUsedMB: swap, MemoryPressure: classifyPressure(totalMB, available, swap)}
}

func classifyPressure(totalMB, availableMB, swapMB int) string {
	if totalMB <= 0 || availableMB <= 0 {
		return "unknown"
	}
	ratio := float64(availableMB) / float64(totalMB)
	if ratio < 0.10 || (swapMB > 4096 && ratio < 0.18) {
		return "critical"
	}
	if ratio < 0.22 || (swapMB > 2048 && ratio < 0.30) {
		return "warning"
	}
	return "normal"
}

func sysctl(ctx context.Context, key string) string {
	out, err := exec.CommandContext(ctx, "sysctl", "-n", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func darwinAvailableMemoryMB(ctx context.Context) int {
	out, err := exec.CommandContext(ctx, "vm_stat").Output()
	if err != nil {
		return 0
	}
	pageSize := int64(4096)
	var reclaimablePages int64
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "page size of") {
			fields := strings.Fields(line)
			for i, field := range fields {
				if field == "of" && i+1 < len(fields) {
					pageSize, _ = strconv.ParseInt(fields[i+1], 10, 64)
				}
			}
		}
		if strings.HasPrefix(line, "Pages free:") || strings.HasPrefix(line, "Pages inactive:") || strings.HasPrefix(line, "Pages speculative:") || strings.HasPrefix(line, "Pages purgeable:") {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				value := strings.TrimSuffix(parts[len(parts)-1], ".")
				pages, _ := strconv.ParseInt(value, 10, 64)
				reclaimablePages += pages
			}
		}
	}
	return int(reclaimablePages * pageSize / 1024 / 1024)
}

func darwinSwapUsedMB(ctx context.Context) int {
	fields := strings.Fields(sysctl(ctx, "vm.swapusage"))
	for i, field := range fields {
		if field == "used" && i+2 < len(fields) && fields[i+1] == "=" {
			value := fields[i+2]
			if strings.HasSuffix(value, "M") {
				v, err := strconv.ParseFloat(strings.TrimSuffix(value, "M"), 64)
				if err == nil {
					return int(v)
				}
			}
		}
	}
	return 0
}

func linuxMemoryStats() (int, int, int) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, 0
	}
	defer file.Close()
	values := map[string]int{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) < 2 {
			continue
		}
		value, _ := strconv.Atoi(parts[1])
		values[strings.TrimSuffix(parts[0], ":")] = value / 1024
	}
	available := values["MemAvailable"]
	if available == 0 {
		available = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	swapUsed := max(0, values["SwapTotal"]-values["SwapFree"])
	return values["MemTotal"], available, swapUsed
}

func (p Profile) String() string {
	return fmt.Sprintf("%s (%s, %d MB)", p.Name, p.Chip, p.TotalMemoryMB)
}
