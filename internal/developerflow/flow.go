package developerflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	contextpkg "github.com/ulaista/usage-ai-on-dev/internal/context"
	"github.com/ulaista/usage-ai-on-dev/internal/core"
	"github.com/ulaista/usage-ai-on-dev/internal/domain"
	"github.com/ulaista/usage-ai-on-dev/internal/hardware"
	"github.com/ulaista/usage-ai-on-dev/internal/localworker"
	"github.com/ulaista/usage-ai-on-dev/internal/projectmode"
	"github.com/ulaista/usage-ai-on-dev/internal/router"
	"github.com/ulaista/usage-ai-on-dev/internal/verification"
)

type SemanticEvidence struct { Query string `json:"query"`; Result domain.SemanticResult `json:"result"` }

type Plan struct {
	SessionID            string                  `json:"session_id"`
	Task                 string                  `json:"task"`
	Project              projectmode.Project     `json:"project"`
	Baseline             projectmode.Baseline    `json:"baseline"`
	Impact               projectmode.Impact      `json:"impact"`
	Semantic             []SemanticEvidence      `json:"semantic_evidence,omitempty"`
	Context              contextpkg.CachedPacket `json:"context"`
	Route                router.Decision         `json:"route"`
	MechanicalOperations int                     `json:"mechanical_operations"`
	PlannedLocalAICalls  int                     `json:"planned_local_ai_calls"`
	PlannedStrongAICalls int                     `json:"planned_strong_ai_calls"`
	PreserveUserDirty    bool                    `json:"preserve_user_dirty"`
	Warnings             []string                `json:"warnings,omitempty"`
}

type RunResult struct {
	Plan                 Plan                  `json:"plan"`
	LocalResult          *localworker.Result   `json:"local_result,omitempty"`
	VerificationCapsule  *verification.Capsule `json:"verification_capsule,omitempty"`
	VerificationMarkdown string                `json:"verification_markdown,omitempty"`
	StrongOwnership      bool                  `json:"strong_ownership_required"`
	NoAI                 bool                  `json:"no_ai"`
}

type Engine struct{ Service *core.Service }

func (e Engine) Prepare(ctx context.Context, sessionID, task string) (Plan, error) {
	if e.Service == nil { return Plan{}, fmt.Errorf("service is required") }
	if strings.TrimSpace(task)=="" { return Plan{}, fmt.Errorf("task is required") }
	if strings.TrimSpace(sessionID)=="" { return Plan{}, fmt.Errorf("session_id is required") }
	pm:=projectmode.Engine{Root:e.Service.Config.Root,StateDir:e.Service.Config.StateDir}
	baseline,err:=pm.Begin(ctx,sessionID,task);if err!=nil{return Plan{},err}
	impact,err:=pm.Impact(ctx,sessionID);if err!=nil{return Plan{},err}
	mechanical:=8
	semanticEvidence:=[]SemanticEvidence{}
	if baseline.Project.Mode=="existing"&&e.Service.Semantic!=nil{for _,term:=range semanticTerms(task,4){result,findErr:=e.Service.Semantic.FindSymbol(ctx,domain.SymbolQuery{Pattern:term,Depth:1});mechanical++;if findErr==nil&&strings.TrimSpace(result.Raw)!=""{semanticEvidence=append(semanticEvidence,SemanticEvidence{Query:term,Result:result})}}}
	view,err:=hardware.Review(ctx,e.Service.Config.StateDir);if err!=nil{return Plan{},err};mechanical++
	compiled,err:=(contextpkg.Compiler{Service:e.Service}).CompileCached(ctx,task,view.Effective.SoftContextTokens);if err!=nil{return Plan{},err};mechanical++
	model:=e.Service.Config.LocalModel;if view.Effective.PreferredModel!=""{model=view.Effective.PreferredModel}
	route,err:=(router.Router{Stats:e.Service.Store}).Decide(ctx,router.Request{Task:task,TaskType:baseline.TaskKind,Model:model,ContextTokens:compiled.Packet.EstimatedTokens,ProjectMode:baseline.Project.Mode,DiscoveryReady:impact.DiscoveryReady});if err!=nil{return Plan{},err};mechanical++
	local,strong:=plannedAI(route);warnings:=[]string{}
	if len(baseline.UserDirty)>0{warnings=append(warnings,"pre-existing developer changes are protected as USER_DIRTY and must not be reverted or overwritten implicitly")}
	if len(impact.Ownership.Conflicts)>0{warnings=append(warnings,"files changed after baseline overlap USER_DIRTY; require explicit conflict-aware review before applying edits")}
	if baseline.Project.Mode=="existing"&&len(semanticEvidence)==0{warnings=append(warnings,"semantic evidence is unavailable or empty; continue with repo-map/context evidence and require stronger verification for broad changes")}
	return Plan{SessionID:sessionID,Task:task,Project:baseline.Project,Baseline:baseline,Impact:impact,Semantic:semanticEvidence,Context:compiled,Route:route,MechanicalOperations:mechanical,PlannedLocalAICalls:local,PlannedStrongAICalls:strong,PreserveUserDirty:true,Warnings:warnings},nil
}

func (e Engine) Run(ctx context.Context, sessionID, task string) (RunResult, error) {
	plan,err:=e.Prepare(ctx,sessionID,task);if err!=nil{return RunResult{},err}
	out:=RunResult{Plan:plan,StrongOwnership:plan.Route.Route=="strong",NoAI:plan.Route.NoAI||plan.Route.Route=="mechanical"}
	if out.StrongOwnership||out.NoAI{return out,nil}
	if plan.Project.Mode=="existing"&&!plan.Impact.DiscoveryReady{return RunResult{},fmt.Errorf("existing project discovery contract is not ready")}
	if len(plan.Impact.Ownership.Conflicts)>0{out.StrongOwnership=true;out.Plan.Warnings=append(out.Plan.Warnings,"local execution suppressed because USER_DIRTY overlap changed after baseline");return out,nil}
	view,err:=hardware.Review(ctx,e.Service.Config.StateDir);if err!=nil{return RunResult{},err}
	model:=e.Service.Config.LocalModel;if view.Effective.PreferredModel!=""{model=view.Effective.PreferredModel}
	worker:=localworker.Worker{Model:model,OllamaURL:e.Service.Config.OllamaURL,StateDir:e.Service.Config.StateDir,Store:e.Service.Store,Limits:&view.Effective}
	result,err:=worker.Run(ctx,localworker.Request{Task:task,TaskType:plan.Route.TaskType,Context:plan.Context.Packet.RenderMarkdown(),ContextTokens:plan.Context.Packet.EstimatedTokens,ProjectMode:plan.Project.Mode,DiscoveryReady:plan.Impact.DiscoveryReady});if err!=nil{return RunResult{},err}
	out.LocalResult=&result;out.StrongOwnership=result.FallbackRequired&&result.RouteDecision.Route=="strong"
	capsule:=verification.Build(verification.BuildRequest{Task:task,StateID:plan.Context.StateID,ContextKey:plan.Context.Key,Packet:plan.Context.Packet,Result:result,MaxTokens:2500});out.VerificationCapsule=&capsule;out.VerificationMarkdown=capsule.RenderMarkdown()
	if !result.FallbackRequired&&capsule.EstimatedTokenSaving>0{_=e.Service.Store.RecordSaving(ctx,domain.CacheSaving{Kind:"verification",Key:capsule.Fingerprint(),SavedInputTokens:capsule.EstimatedTokenSaving,CreatedAt:time.Now().UTC()})}
	return out,nil
}

func plannedAI(d router.Decision)(int,int){switch d.Route{case "mechanical":return 0,0;case "local":return 1,0;case "local-verify":return 1,1;default:return 0,1}}
func semanticTerms(task string,limit int)[]string{stop:=map[string]bool{"the":true,"and":true,"for":true,"with":true,"add":true,"fix":true,"bug":true,"this":true,"that":true,"нужно":true,"сделать":true,"добавить":true,"починить":true,"фикс":true,"проект":true};seen:=map[string]bool{};out:=[]string{};for _,term:=range strings.FieldsFunc(strings.ToLower(task),func(r rune)bool{return !(r>='a'&&r<='z'||r>='0'&&r<='9'||r>='а'&&r<='я')}){if len([]rune(term))<3||stop[term]||seen[term]{continue};seen[term]=true;out=append(out,term);if len(out)>=limit{break}};return out}
