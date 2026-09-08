package hardware

import "testing"

func TestSelectM2Air16Profile(t *testing.T) {
	p := SelectProfile("darwin", "Apple M2", 16*1024)
	if p.Name != "apple-lite" {
		t.Fatalf("expected apple-lite, got %s", p.Name)
	}
	if !p.ThermalSensitive {
		t.Fatal("M2 Air style profile should be thermal sensitive")
	}
	if p.MaxParallelWorkers != 1 || p.SoftContextTokens != 6000 || p.HardContextTokens != 12000 {
		t.Fatalf("unexpected M2 profile: %#v", p)
	}
}

func TestSelectM3Pro18Profile(t *testing.T) {
	p := SelectProfile("darwin", "Apple M3 Pro", 18*1024)
	if p.Name != "apple-balanced" {
		t.Fatalf("expected apple-balanced, got %s", p.Name)
	}
	if p.ThermalSensitive {
		t.Fatal("M3 Pro should not use Air thermal policy")
	}
	if p.SoftContextTokens != 8000 || p.HardContextTokens != 16000 {
		t.Fatalf("unexpected M3 Pro context limits: %#v", p)
	}
}

func TestResourceGuardRejectsSwapStormRisk(t *testing.T) {
	p := SelectProfile("darwin", "Apple M2", 16*1024)
	decision := p.Evaluate(RuntimeResources{AvailableMemoryMB: 6500, SwapUsedMB: 3000, MemoryPressure: "warning"}, 6000)
	if decision.Allowed {
		t.Fatalf("expected local inference rejection, got %#v", decision)
	}
}

func TestResourceGuardShrinksContextUnderWarning(t *testing.T) {
	p := SelectProfile("darwin", "Apple M3 Pro", 18*1024)
	decision := p.Evaluate(RuntimeResources{AvailableMemoryMB: 9000, SwapUsedMB: 300, MemoryPressure: "warning"}, 8000)
	if !decision.Allowed {
		t.Fatalf("expected local inference allowed, got %#v", decision)
	}
	if decision.RecommendedContextTokens >= 8000 {
		t.Fatalf("expected reduced context under pressure, got %#v", decision)
	}
}

func TestResourceGuardRejectsOversizedContext(t *testing.T) {
	p := SelectProfile("darwin", "Apple M2", 16*1024)
	decision := p.Evaluate(RuntimeResources{AvailableMemoryMB: 12000, MemoryPressure: "normal"}, 20000)
	if decision.Allowed || decision.RecommendedContextTokens != 12000 {
		t.Fatalf("expected hard-context rejection, got %#v", decision)
	}
}
