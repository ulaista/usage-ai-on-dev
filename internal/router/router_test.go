package router

import (
	"context"
	"testing"

	"github.com/ulaista/usage-ai-on-dev/internal/domain"
)

type fakeStats struct{ stats domain.WorkloadStats }
func (f fakeStats) WorkloadStats(context.Context,string,string)(domain.WorkloadStats,error){return f.stats,nil}

func TestSecurityAlwaysRoutesStrong(t *testing.T){
	r:=Router{Stats:fakeStats{stats:domain.WorkloadStats{Samples:100,Reviewed:100,AcceptanceRate:1}}}
	d,err:=r.Decide(context.Background(),Request{Task:"change JWT validation in auth middleware",Model:"qwen"});if err!=nil{t.Fatal(err)}
	if d.Route!="strong"||!d.HardGate{t.Fatalf("security task must hard route strong: %#v",d)}
}
func TestCacheHitCanAvoidAI(t *testing.T){
	d,err:=(Router{}).Decide(context.Background(),Request{Task:"where is Login used",CacheHit:true});if err!=nil{t.Fatal(err)}
	if d.Route!="mechanical"||!d.NoAI{t.Fatalf("cache hit should avoid AI: %#v",d)}
}
func TestColdWorkloadUsesLocalWithVerification(t *testing.T){
	r:=Router{Stats:fakeStats{stats:domain.WorkloadStats{TaskType:"tests",Samples:2,Reviewed:1,AcceptanceRate:1}}}
	d,err:=r.Decide(context.Background(),Request{Task:"add tests for parser",Model:"qwen"});if err!=nil{t.Fatal(err)}
	if d.Route!="local-verify"||!d.RequireStrongReview{t.Fatalf("cold workload should require verifier: %#v",d)}
}
func TestReliableWorkloadCanSkipStrongReview(t *testing.T){
	r:=Router{Stats:fakeStats{stats:domain.WorkloadStats{TaskType:"tests",Samples:30,Reviewed:20,Accepted:20,AcceptanceRate:1,FallbackRate:.02,ErrorRate:.01}}}
	d,err:=r.Decide(context.Background(),Request{Task:"add tests for parser",Model:"qwen"});if err!=nil{t.Fatal(err)}
	if d.Route!="local"||d.RequireStrongReview{t.Fatalf("mature reliable workload should use local-only route: %#v",d)}
}
func TestUnreliableWorkloadEscalates(t *testing.T){
	r:=Router{Stats:fakeStats{stats:domain.WorkloadStats{TaskType:"bugfix",Samples:20,Reviewed:10,AcceptanceRate:.6,FallbackRate:.3,ErrorRate:.1}}}
	d,err:=r.Decide(context.Background(),Request{Task:"fix parser bug",Model:"qwen"});if err!=nil{t.Fatal(err)}
	if d.Route!="strong"{t.Fatalf("unreliable local workload should escalate: %#v",d)}
}
