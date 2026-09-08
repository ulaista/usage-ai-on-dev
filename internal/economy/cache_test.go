package economy

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFingerprintStableAndSensitive(t *testing.T) {
	a := Fingerprint("task", "state-a", "model")
	b := Fingerprint("task", "state-a", "model")
	c := Fingerprint("task", "state-b", "model")
	if a != b { t.Fatalf("same inputs must produce same fingerprint") }
	if a == c { t.Fatalf("state change must invalidate fingerprint") }
	if NormalizeTask("  fix   auth\n token ") != "fix auth token" { t.Fatalf("task normalization failed") }
}

func TestGroupCoalescesConcurrentWork(t *testing.T) {
	var g Group
	var calls atomic.Int32
	const workers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(workers)
	results := make([]int, workers)
	for i := 0; i < workers; i++ {
		go func(index int) {
			defer wg.Done()
			<-start
			value, err, _ := g.Do("same-key", func() (any, error) {
				calls.Add(1)
				time.Sleep(30 * time.Millisecond)
				return 42, nil
			})
			if err != nil { t.Errorf("unexpected error: %v", err); return }
			results[index] = value.(int)
		}(i)
	}
	close(start)
	wg.Wait()
	if calls.Load() != 1 { t.Fatalf("expected one real execution, got %d", calls.Load()) }
	for _, value := range results { if value != 42 { t.Fatalf("unexpected shared result %d", value) } }
}
