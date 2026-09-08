package hardware

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

type GPUInfo struct {
	Name     string `json:"name"`
	Vendor   string `json:"vendor,omitempty"`
	VRAMMB   int    `json:"vram_mb,omitempty"`
	Unified  bool   `json:"unified_memory,omitempty"`
	Backend  string `json:"backend,omitempty"`
}

type Inventory struct {
	HardwareID   string    `json:"hardware_id"`
	OS           string    `json:"os"`
	Architecture string    `json:"architecture"`
	CPU          string    `json:"cpu"`
	LogicalCPUs  int       `json:"logical_cpus"`
	TotalMemoryMB int      `json:"total_memory_mb"`
	GPUs         []GPUInfo `json:"gpus,omitempty"`
}

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
	Inventory Inventory        `json:"inventory"`
	Profile   Profile          `json:"recommended_profile"`
	Resources RuntimeResources `json:"resources"`
}

type Detector interface {
	Snapshot(context.Context) (Snapshot, error)
}

type SystemDetector struct{}

func (SystemDetector) Snapshot(ctx context.Context) (Snapshot, error) {
	inventory := detectInventory(ctx)
	profile := SelectProfile(inventory.OS, inventory.CPU, inventory.TotalMemoryMB)
	profile.PreferredModelClass = recommendModelClass(inventory)
	resources := detectRuntimeResources(ctx, inventory.TotalMemoryMB)
	return Snapshot{Inventory: inventory, Profile: profile, Resources: resources}, nil
}

func SelectProfile(platform, chip string, totalMB int) Profile {
	p := Profile{
		Name: "auto-conservative", Platform: platform, Chip: chip, TotalMemoryMB: totalMB,
		MemoryReserveMB: max(3072, totalMB/3), SoftContextTokens: 4096, HardContextTokens: 8192,
		MaxOutputTokens: 800, MaxParallelWorkers: 1, PreferredModelClass: "3b-4b-q4",
	}
	if totalMB >= 12*1024 {
		p.Name = "auto-lite"
		p.MemoryReserveMB = max(4096, totalMB/3)
		p.SoftContextTokens = 6000
		p.HardContextTokens = 12000
		p.MaxOutputTokens = 1000
		p.PreferredModelClass = "4b-q4"
	}
	if totalMB >= 17*1024 {
		p.Name = "auto-balanced"
		p.MemoryReserveMB = max(5000, totalMB/4)
		p.SoftContextTokens = 8000
		p.HardContextTokens = 16000
		p.MaxOutputTokens = 1500
		p.PreferredModelClass = "4b-8b-q4"
	}
	if totalMB >= 32*1024 {
		p.Name = "auto-capable"
		p.MemoryReserveMB = max(6144, totalMB/5)
		p.SoftContextTokens = 12000
		p.HardContextTokens = 24000
		p.MaxOutputTokens = 2000
		p.MaxParallelWorkers = 2
		p.PreferredModelClass = "8b-14b-q4"
	}
	lower := strings.ToLower(chip)
	p.ThermalSensitive = platform == "darwin" && strings.Contains(lower, "apple m") && !strings.Contains(lower, "pro") && !strings.Contains(lower, "max") && !strings.Contains(lower, "ultra")
	return p
}

func recommendModelClass(in Inventory) string {
	bestVRAM := 0
	hasUnified := false
	for _, gpu := range in.GPUs {
		if gpu.VRAMMB > bestVRAM { bestVRAM = gpu.VRAMMB }
		if gpu.Unified { hasUnified = true }
	}
	if hasUnified {
		switch {
		case in.TotalMemoryMB >= 48*1024: return "14b-32b-q4"
		case in.TotalMemoryMB >= 24*1024: return "8b-14b-q4"
		case in.TotalMemoryMB >= 16*1024: return "4b-8b-q4"
		default: return "3b-4b-q4"
		}
	}
	switch {
	case bestVRAM >= 24000: return "14b-32b-q4"
	case bestVRAM >= 12000: return "8b-14b-q4"
	case bestVRAM >= 7000: return "4b-8b-q4"
	case bestVRAM > 0: return "3b-4b-q4"
	default: return SelectProfile(in.OS, in.CPU, in.TotalMemoryMB).PreferredModelClass
	}
}

func (p Profile) Evaluate(r RuntimeResources, requestedContext int) Decision {
	if requestedContext <= 0 { requestedContext = p.SoftContextTokens }
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
	if r.MemoryPressure == "warning" || usable < 3072 { recommended = min(recommended, max(2048, p.SoftContextTokens/2)) }
	return Decision{Allowed: true, RecommendedContextTokens: recommended, Reason: "local execution fits current hardware budget"}
}

type Decision struct {
	Allowed                  bool   `json:"allowed"`
	RecommendedContextTokens int    `json:"recommended_context_tokens"`
	Reason                   string `json:"reason"`
}

func detectInventory(ctx context.Context) Inventory {
	cpu, totalMB := detectPlatform(ctx)
	inv := Inventory{OS: runtime.GOOS, Architecture: runtime.GOARCH, CPU: cpu, LogicalCPUs: runtime.NumCPU(), TotalMemoryMB: totalMB}
	inv.GPUs = detectGPUs(ctx)
	hash := sha256.Sum256([]byte(strings.Join([]string{inv.OS, inv.Architecture, inv.CPU, strconv.Itoa(inv.TotalMemoryMB), gpuFingerprint(inv.GPUs)}, "|")))
	inv.HardwareID = hex.EncodeToString(hash[:8])
	return inv
}

func gpuFingerprint(gpus []GPUInfo) string {
	parts := make([]string, 0, len(gpus))
	for _, gpu := range gpus { parts = append(parts, fmt.Sprintf("%s:%d:%t", gpu.Name, gpu.VRAMMB, gpu.Unified)) }
	return strings.Join(parts, ",")
}

func detectPlatform(ctx context.Context) (string, int) {
	switch runtime.GOOS {
	case "darwin":
		chip := sysctl(ctx, "machdep.cpu.brand_string")
		if chip == "" { chip = sysctl(ctx, "hw.model") }
		total, _ := strconv.ParseInt(strings.TrimSpace(sysctl(ctx, "hw.memsize")), 10, 64)
		return chip, int(total / 1024 / 1024)
	case "windows":
		cpu := powerShell(ctx, "(Get-CimInstance Win32_Processor | Select-Object -First 1 -ExpandProperty Name)")
		total, _ := strconv.ParseInt(strings.TrimSpace(powerShell(ctx, "[int64](Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory")), 10, 64)
		return cpu, int(total / 1024 / 1024)
	default:
		total, _, _ := linuxMemoryStats()
		cpu := linuxCPUName()
		if cpu == "" { cpu = runtime.GOARCH }
		return cpu, total
	}
}

func detectGPUs(ctx context.Context) []GPUInfo {
	if runtime.GOOS == "darwin" {
		name := sysctl(ctx, "machdep.cpu.brand_string")
		if strings.Contains(strings.ToLower(name), "apple") {
			return []GPUInfo{{Name: name + " integrated GPU", Vendor: "Apple", Unified: true, Backend: "Metal"}}
		}
	}
	if out, err := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=name,memory.total", "--format=csv,noheader,nounits").Output(); err == nil {
		var result []GPUInfo
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			parts := strings.Split(line, ",")
			if len(parts) >= 2 {
				vram, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
				result = append(result, GPUInfo{Name: strings.TrimSpace(parts[0]), Vendor: "NVIDIA", VRAMMB: vram, Backend: "CUDA"})
			}
		}
		if len(result) > 0 { return result }
	}
	if runtime.GOOS == "windows" {
		name := powerShell(ctx, "(Get-CimInstance Win32_VideoController | Select-Object -First 1 -ExpandProperty Name)")
		if name != "" { return []GPUInfo{{Name: name, Backend: "system"}} }
	}
	return nil
}

func detectRuntimeResources(ctx context.Context, totalMB int) RuntimeResources {
	switch runtime.GOOS {
	case "darwin":
		available := darwinAvailableMemoryMB(ctx); swap := darwinSwapUsedMB(ctx)
		return RuntimeResources{AvailableMemoryMB: available, SwapUsedMB: swap, MemoryPressure: classifyPressure(totalMB, available, swap)}
	case "windows":
		availableKB, _ := strconv.Atoi(strings.TrimSpace(powerShell(ctx, "(Get-CimInstance Win32_OperatingSystem).FreePhysicalMemory")))
		available := availableKB / 1024
		return RuntimeResources{AvailableMemoryMB: available, MemoryPressure: classifyPressure(totalMB, available, 0)}
	default:
		_, available, swap := linuxMemoryStats()
		return RuntimeResources{AvailableMemoryMB: available, SwapUsedMB: swap, MemoryPressure: classifyPressure(totalMB, available, swap)}
	}
}

func classifyPressure(totalMB, availableMB, swapMB int) string {
	if totalMB <= 0 || availableMB <= 0 { return "unknown" }
	ratio := float64(availableMB) / float64(totalMB)
	if ratio < 0.10 || (swapMB > 4096 && ratio < 0.18) { return "critical" }
	if ratio < 0.22 || (swapMB > 2048 && ratio < 0.30) { return "warning" }
	return "normal"
}

func sysctl(ctx context.Context, key string) string {
	out, err := exec.CommandContext(ctx, "sysctl", "-n", key).Output(); if err != nil { return "" }
	return strings.TrimSpace(string(out))
}

func powerShell(ctx context.Context, command string) string {
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", command).Output(); if err != nil { return "" }
	return strings.TrimSpace(string(out))
}

func linuxCPUName() string {
	file, err := os.Open("/proc/cpuinfo"); if err != nil { return "" }; defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "model name") || strings.HasPrefix(line, "Hardware") {
			if parts := strings.SplitN(line, ":", 2); len(parts) == 2 { return strings.TrimSpace(parts[1]) }
		}
	}
	return ""
}

func darwinAvailableMemoryMB(ctx context.Context) int {
	out, err := exec.CommandContext(ctx, "vm_stat").Output(); if err != nil { return 0 }
	pageSize := int64(4096); var reclaimablePages int64
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "page size of") {
			fields := strings.Fields(line); for i, field := range fields { if field == "of" && i+1 < len(fields) { pageSize, _ = strconv.ParseInt(fields[i+1], 10, 64) } }
		}
		if strings.HasPrefix(line, "Pages free:") || strings.HasPrefix(line, "Pages inactive:") || strings.HasPrefix(line, "Pages speculative:") || strings.HasPrefix(line, "Pages purgeable:") {
			parts := strings.Fields(line); if len(parts) > 0 { value := strings.TrimSuffix(parts[len(parts)-1], "."); pages, _ := strconv.ParseInt(value, 10, 64); reclaimablePages += pages }
		}
	}
	return int(reclaimablePages * pageSize / 1024 / 1024)
}

func darwinSwapUsedMB(ctx context.Context) int {
	fields := strings.Fields(sysctl(ctx, "vm.swapusage"))
	for i, field := range fields {
		if field == "used" && i+2 < len(fields) && fields[i+1] == "=" {
			value := fields[i+2]; if strings.HasSuffix(value, "M") { v, err := strconv.ParseFloat(strings.TrimSuffix(value, "M"), 64); if err == nil { return int(v) } }
		}
	}
	return 0
}

func linuxMemoryStats() (int, int, int) {
	file, err := os.Open("/proc/meminfo"); if err != nil { return 0, 0, 0 }; defer file.Close()
	values := map[string]int{}; scanner := bufio.NewScanner(file)
	for scanner.Scan() { parts := strings.Fields(scanner.Text()); if len(parts) < 2 { continue }; value, _ := strconv.Atoi(parts[1]); values[strings.TrimSuffix(parts[0], ":")] = value / 1024 }
	available := values["MemAvailable"]; if available == 0 { available = values["MemFree"] + values["Buffers"] + values["Cached"] }
	swapUsed := max(0, values["SwapTotal"]-values["SwapFree"])
	return values["MemTotal"], available, swapUsed
}

func (p Profile) String() string { return fmt.Sprintf("%s (%s, %d MB)", p.Name, p.Chip, p.TotalMemoryMB) }
