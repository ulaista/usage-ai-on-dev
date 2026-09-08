package hardware

import "testing"

func TestAutoProfileFor16GBMachine(t *testing.T) {
	p := SelectProfile("darwin", "Apple M2", 16*1024)
	if p.Name != "auto-lite" { t.Fatalf("expected auto-lite, got %s", p.Name) }
	if !p.ThermalSensitive { t.Fatal("fanless-class Apple M chip should be thermal sensitive") }
	if p.MaxParallelWorkers != 1 || p.SoftContextTokens != 6000 || p.HardContextTokens != 12000 { t.Fatalf("unexpected 16GB profile: %#v", p) }
}

func TestAutoProfileFor18GBMachine(t *testing.T) {
	p := SelectProfile("darwin", "Apple M3 Pro", 18*1024)
	if p.Name != "auto-balanced" { t.Fatalf("expected auto-balanced, got %s", p.Name) }
	if p.ThermalSensitive { t.Fatal("Pro-class chip should not use fanless thermal policy") }
	if p.SoftContextTokens != 8000 || p.HardContextTokens != 16000 { t.Fatalf("unexpected 18GB context limits: %#v", p) }
}

func TestAutoProfileIsPlatformIndependent(t *testing.T) {
	linux := SelectProfile("linux", "AMD Ryzen", 18*1024)
	windows := SelectProfile("windows", "Intel Core", 18*1024)
	if linux.Name != windows.Name || linux.SoftContextTokens != windows.SoftContextTokens { t.Fatalf("same resource class should get same base limits: linux=%#v windows=%#v", linux, windows) }
}

func TestResourceGuardRejectsSwapStormRisk(t *testing.T) {
	p := SelectProfile("linux", "x86_64", 16*1024)
	decision := p.Evaluate(RuntimeResources{AvailableMemoryMB:6500,SwapUsedMB:3000,MemoryPressure:"warning"},6000)
	if decision.Allowed { t.Fatalf("expected local inference rejection, got %#v",decision) }
}

func TestResourceGuardShrinksContextUnderWarning(t *testing.T) {
	p := SelectProfile("windows", "generic", 18*1024)
	decision := p.Evaluate(RuntimeResources{AvailableMemoryMB:9000,SwapUsedMB:300,MemoryPressure:"warning"},8000)
	if !decision.Allowed { t.Fatalf("expected local inference allowed, got %#v",decision) }
	if decision.RecommendedContextTokens>=8000 { t.Fatalf("expected reduced context under pressure, got %#v",decision) }
}

func TestResourceGuardRejectsOversizedContext(t *testing.T) {
	p := SelectProfile("darwin", "Apple M2", 16*1024)
	decision := p.Evaluate(RuntimeResources{AvailableMemoryMB:12000,MemoryPressure:"normal"},20000)
	if decision.Allowed || decision.RecommendedContextTokens!=12000 { t.Fatalf("expected hard-context rejection, got %#v",decision) }
}
