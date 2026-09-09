package router

import (
	"context"
	"testing"
)

func TestExistingProjectRequiresDiscoveryBeforeLocalAI(t *testing.T) {
	decision,err:=(Router{}).Decide(context.Background(),Request{Task:"fix parser bug",TaskType:"bugfix",ProjectMode:"existing",DiscoveryReady:false})
	if err!=nil{t.Fatal(err)}
	if decision.Route!="strong"||!decision.HardGate{t.Fatalf("existing project without discovery must not reach local AI: %#v",decision)}
	if decision.Reason==""{t.Fatalf("expected explicit discovery reason")}
}
