package developerflow

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ulaista/usage-ai-on-dev/internal/config"
	"github.com/ulaista/usage-ai-on-dev/internal/core"
)

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil { t.Fatalf("git %v: %v: %s", args, err, out) }
}

func TestPrepareExistingProjectPreservesDirtyAndPlansColdBugfix(t *testing.T) {
	root := t.TempDir()
	runGit(t,root,"init");runGit(t,root,"config","user.email","brain@example.test");runGit(t,root,"config","user.name","Project Brain")
	_ = os.WriteFile(filepath.Join(root,"go.mod"),[]byte("module example.com/app\n\ngo 1.25\n"),0o644)
	_ = os.WriteFile(filepath.Join(root,"parser.go"),[]byte("package app\nfunc Parse(){}\n"),0o644)
	_ = os.WriteFile(filepath.Join(root,"parser_test.go"),[]byte("package app\nfunc TestParse(){}\n"),0o644)
	runGit(t,root,"add",".");runGit(t,root,"commit","-m","initial parser")
	_ = os.WriteFile(filepath.Join(root,"README.md"),[]byte("developer local note\n"),0o644)

	cfg:=config.Default(root)
	cfg.SemanticProvider=""
	svc,err:=core.Open(context.Background(),cfg,false);if err!=nil{t.Fatal(err)}
	defer svc.Close()
	plan,err:=(Engine{Service:svc}).Prepare(context.Background(),"dev-session","fix parser regression");if err!=nil{t.Fatal(err)}
	if plan.Project.Mode!="existing"{t.Fatalf("expected existing project: %#v",plan.Project)}
	if !plan.PreserveUserDirty{t.Fatalf("developer dirty work must be protected")}
	if !has(plan.Baseline.UserDirty,"README.md"){t.Fatalf("expected README.md in USER_DIRTY: %#v",plan.Baseline.UserDirty)}
	if !has(plan.Impact.AffectedFiles,"parser.go"){t.Fatalf("expected parser.go affected: %#v",plan.Impact.AffectedFiles)}
	if !has(plan.Impact.RelatedTests,"parser_test.go"){t.Fatalf("expected parser test discovery: %#v",plan.Impact.RelatedTests)}
	if !plan.Impact.DiscoveryReady{t.Fatalf("existing project discovery should be ready: %#v",plan.Impact.Missing)}
	if plan.MechanicalOperations < 10{t.Fatalf("expected mechanical recovery operations, got %d",plan.MechanicalOperations)}
	if plan.Route.Route!="local-verify"{t.Fatalf("cold low-risk bugfix should use local + strong verifier, got %#v",plan.Route)}
	if plan.PlannedLocalAICalls!=1||plan.PlannedStrongAICalls!=1{t.Fatalf("unexpected planned AI calls: local=%d strong=%d",plan.PlannedLocalAICalls,plan.PlannedStrongAICalls)}
}

func has(values []string,wanted string)bool{for _,v:=range values{if v==wanted{return true}};return false}
