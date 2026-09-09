package projectmode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEnsureKeepsOriginalDirtyOwnershipAcrossIterations(t *testing.T) {
	root := t.TempDir()
	cmd := func(args ...string) { t.Helper(); c:=exec.Command("git",append([]string{"-C",root},args...)...); if out,err:=c.CombinedOutput();err!=nil{t.Fatalf("git %v: %v: %s",args,err,out)} }
	cmd("init");cmd("config","user.email","brain@example.test");cmd("config","user.name","Project Brain")
	if err:=os.WriteFile(filepath.Join(root,"go.mod"),[]byte("module example.com/app\n\ngo 1.25\n"),0o644);err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(filepath.Join(root,"app.go"),[]byte("package app\nfunc Run(){}\n"),0o644);err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(filepath.Join(root,"app_test.go"),[]byte("package app\nfunc TestRun(){}\n"),0o644);err!=nil{t.Fatal(err)}
	cmd("add",".");cmd("commit","-m","initial")

	// Developer edit exists before Project Brain starts.
	if err:=os.WriteFile(filepath.Join(root,"app.go"),[]byte("package app\nfunc Run(){ /* developer */ }\n"),0o644);err!=nil{t.Fatal(err)}
	e:=Engine{Root:root,StateDir:filepath.Join(root,".project-brain")};ctx:=context.Background()
	first,err:=e.Ensure(ctx,"stable-session","fix run regression");if err!=nil{t.Fatal(err)}
	if !containsString(first.UserDirty,"app.go"){t.Fatalf("expected original developer edit in USER_DIRTY: %#v",first.UserDirty)}

	// Work performed after the baseline must remain BRAIN_DELTA on later planning calls.
	if err:=os.WriteFile(filepath.Join(root,"app_test.go"),[]byte("package app\nfunc TestRun(){}\nfunc TestRegression(){}\n"),0o644);err!=nil{t.Fatal(err)}
	second,err:=e.Ensure(ctx,"stable-session","fix run regression");if err!=nil{t.Fatal(err)}
	if len(second.UserDirty)!=len(first.UserDirty)||!containsString(second.UserDirty,"app.go"){t.Fatalf("baseline was unexpectedly recaptured: first=%#v second=%#v",first.UserDirty,second.UserDirty)}
	if containsString(second.UserDirty,"app_test.go"){t.Fatalf("later brain edit must not become USER_DIRTY: %#v",second.UserDirty)}
	impact,err:=e.Impact(ctx,"stable-session");if err!=nil{t.Fatal(err)}
	if !containsString(impact.Ownership.BrainDelta,"app_test.go"){t.Fatalf("expected app_test.go in BRAIN_DELTA: %#v",impact.Ownership)}
}
