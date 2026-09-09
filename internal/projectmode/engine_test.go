package projectmode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil { t.Fatalf("git %v: %v: %s", args, err, out) }
}

func TestExistingProjectOwnershipPreservesPreexistingDirtyFiles(t *testing.T) {
	root := t.TempDir(); state := filepath.Join(root, ".project-brain")
	git(t, root, "init"); git(t, root, "config", "user.email", "brain@example.test"); git(t, root, "config", "user.name", "Project Brain")
	if err:=os.WriteFile(filepath.Join(root,"go.mod"),[]byte("module example.com/app\n\ngo 1.25\n"),0o644);err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(filepath.Join(root,"auth.go"),[]byte("package auth\nfunc Login(){}\n"),0o644);err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(filepath.Join(root,"auth_test.go"),[]byte("package auth\nfunc TestLogin(){}\n"),0o644);err!=nil{t.Fatal(err)}
	git(t,root,"add",".");git(t,root,"commit","-m","initial")
	if err:=os.WriteFile(filepath.Join(root,"auth.go"),[]byte("package auth\nfunc Login(){ /* developer edit */ }\n"),0o644);err!=nil{t.Fatal(err)}

	e:=Engine{Root:root,StateDir:state};ctx:=context.Background()
	b,err:=e.Begin(ctx,"session-a","fix login regression");if err!=nil{t.Fatal(err)}
	if b.Project.Mode!="existing"{t.Fatalf("expected existing project: %#v",b.Project)}
	if len(b.UserDirty)!=1||b.UserDirty[0]!="auth.go"{t.Fatalf("expected auth.go as preexisting user dirty: %#v",b.UserDirty)}
	if err:=os.WriteFile(filepath.Join(root,"auth_test.go"),[]byte("package auth\nfunc TestLogin(){}\nfunc TestRefresh(){}\n"),0o644);err!=nil{t.Fatal(err)}
	impact,err:=e.Impact(ctx,"session-a");if err!=nil{t.Fatal(err)}
	if !containsString(impact.Ownership.UserDirty,"auth.go"){t.Fatalf("missing user dirty ownership: %#v",impact.Ownership)}
	if !containsString(impact.Ownership.BrainDelta,"auth_test.go"){t.Fatalf("expected later test edit in brain delta: %#v",impact.Ownership)}
	if containsString(impact.Ownership.Conflicts,"auth.go"){t.Fatalf("unchanged preexisting user edit should not be conflict: %#v",impact.Ownership)}
}

func TestBugfixImpactFindsTestsAndRegressionWindow(t *testing.T) {
	root:=t.TempDir();git(t,root,"init");git(t,root,"config","user.email","brain@example.test");git(t,root,"config","user.name","Project Brain")
	_ = os.WriteFile(filepath.Join(root,"go.mod"),[]byte("module example.com/app\n\ngo 1.25\n"),0o644)
	_ = os.WriteFile(filepath.Join(root,"login.go"),[]byte("package app\nfunc Login(){}\n"),0o644)
	_ = os.WriteFile(filepath.Join(root,"login_test.go"),[]byte("package app\nfunc TestLogin(){}\n"),0o644)
	git(t,root,"add",".");git(t,root,"commit","-m","add login")
	_ = os.WriteFile(filepath.Join(root,"login.go"),[]byte("package app\nfunc Login(){ println(\"changed\") }\n"),0o644)
	git(t,root,"add","login.go");git(t,root,"commit","-m","change login behavior")
	e:=Engine{Root:root,StateDir:filepath.Join(root,".project-brain")};ctx:=context.Background()
	if _,err:=e.Begin(ctx,"bug-1","fix login regression");err!=nil{t.Fatal(err)}
	impact,err:=e.Impact(ctx,"bug-1");if err!=nil{t.Fatal(err)}
	if impact.TaskKind!="bugfix"{t.Fatalf("expected bugfix: %#v",impact)}
	if !containsString(impact.AffectedFiles,"login.go"){t.Fatalf("expected login.go: %#v",impact.AffectedFiles)}
	if !containsString(impact.RelatedTests,"login_test.go"){t.Fatalf("expected related test: %#v",impact.RelatedTests)}
	if len(impact.RegressionWindow)==0{t.Fatalf("expected regression history: %#v",impact)}
	if !impact.DiscoveryReady{t.Fatalf("expected discovery ready: %#v",impact.Missing)}
}

func TestBeginReusesImmutableBaselineForSameSession(t *testing.T) {
	root:=t.TempDir();git(t,root,"init");git(t,root,"config","user.email","brain@example.test");git(t,root,"config","user.name","Project Brain")
	_ = os.WriteFile(filepath.Join(root,"go.mod"),[]byte("module example.com/app\n\ngo 1.25\n"),0o644)
	path:=filepath.Join(root,"login.go")
	_ = os.WriteFile(path,[]byte("package app\nfunc Login(){}\n"),0o644)
	git(t,root,"add",".");git(t,root,"commit","-m","initial")
	_ = os.WriteFile(path,[]byte("package app\nfunc Login(){ /* developer work */ }\n"),0o644)
	e:=Engine{Root:root,StateDir:filepath.Join(root,".project-brain")};ctx:=context.Background()
	first,err:=e.Begin(ctx,"stable-session","fix login bug");if err!=nil{t.Fatal(err)}
	firstHash:=first.UserDirtyHashes["login.go"]
	_ = os.WriteFile(path,[]byte("package app\nfunc Login(){ /* changed after baseline */ }\n"),0o644)
	second,err:=e.Begin(ctx,"stable-session","  fix   login bug ");if err!=nil{t.Fatal(err)}
	if !second.CreatedAt.Equal(first.CreatedAt){t.Fatalf("baseline timestamp changed: first=%s second=%s",first.CreatedAt,second.CreatedAt)}
	if second.UserDirtyHashes["login.go"]!=firstHash{t.Fatalf("baseline hash was overwritten: first=%s second=%s",firstHash,second.UserDirtyHashes["login.go"])}
	impact,err:=e.Impact(ctx,"stable-session");if err!=nil{t.Fatal(err)}
	if !containsString(impact.Ownership.Conflicts,"login.go"){t.Fatalf("post-baseline edit to USER_DIRTY file must be a conflict: %#v",impact.Ownership)}
	if _,err:=e.Begin(ctx,"stable-session","implement unrelated feature");err==nil{t.Fatal("expected session reuse with a different task to fail")}
}

func containsString(values []string, wanted string) bool { for _,v:=range values{if v==wanted{return true}};return false }
