package hardware

import "testing"

func testSnapshot(id string) Snapshot {
	return Snapshot{
		Inventory: Inventory{HardwareID:id,OS:"linux",Architecture:"amd64",CPU:"test",TotalMemoryMB:16*1024},
		Profile: SelectProfile("linux","test",16*1024),
		Resources: RuntimeResources{AvailableMemoryMB:12000,MemoryPressure:"normal"},
	}
}

func TestOverrideRequiresAcceptance(t *testing.T) {
	s := testSnapshot("a")
	view := BuildView(s, Policy{Override:Limits{SoftContextTokens:3000}})
	if !view.RequiresUserAcceptance { t.Fatal("expected acceptance request") }
	if view.Effective.SoftContextTokens != view.Recommended.SoftContextTokens { t.Fatal("unaccepted override must not become effective") }
}

func TestAcceptedOverrideBecomesEffective(t *testing.T) {
	s := testSnapshot("a")
	policy := AcceptPolicy(s, Limits{SoftContextTokens:5000,HardContextTokens:9000,MaxParallelWorkers:1,PreferredModel:"qwen-test"})
	view := BuildView(s,policy)
	if view.RequiresUserAcceptance { t.Fatal("accepted policy should be active") }
	if view.Effective.SoftContextTokens!=5000 || view.Effective.HardContextTokens!=9000 || view.Effective.PreferredModel!="qwen-test" { t.Fatalf("override not applied: %#v",view.Effective) }
}

func TestHardwareChangeInvalidatesAcceptedPolicy(t *testing.T) {
	old := testSnapshot("old")
	policy := AcceptPolicy(old, Limits{HardContextTokens:20000})
	current := testSnapshot("new")
	view := BuildView(current,policy)
	if !view.HardwareChanged || !view.RequiresUserAcceptance { t.Fatal("hardware change must request fresh acceptance") }
	if view.Effective.HardContextTokens != view.Recommended.HardContextTokens { t.Fatal("stale override must not apply to new hardware") }
}

func TestOverrideCannotMakeHardContextSmallerThanSoft(t *testing.T) {
	base := Limits{SoftContextTokens:6000,HardContextTokens:12000}
	out := ApplyOverride(base,Limits{SoftContextTokens:10000,HardContextTokens:8000})
	if out.HardContextTokens < out.SoftContextTokens { t.Fatalf("invalid effective limits: %#v",out) }
}
