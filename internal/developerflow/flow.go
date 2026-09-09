package developerflow

import (
	"context"
	"fmt"
	"strings"

	contextpkg "github.com/ulaista/usage-ai-on-dev/internal/context"
	"github.com/ulaista/usage-ai-on-dev/internal/core"
	"github.com/ulaista/usage-ai-on-dev/internal/domain"
	"github.com/ulaista/usage-ai-on-dev/internal/hardware"
	"github.com/ulaista/usage-ai-on-dev/internal/projectmode"
	"github.com/ulaista/usage-ai-on-dev/internal/router"
)

type SemanticEvidence struct {
	Query  string                `json:"query"`
	Result domain.SemanticResult `json:"result"`
}

type Plan struct {
	SessionID             string                     `json:"session_id"`
	Task                  string                     `json:"task"`
	Project               projectmode.Project        `json:"project"`
	Baseline              projectmode.Baseline       `json:"baseline"`
	Impact                projectmode.Impact         `json:"impact"`
	Semantic              []SemanticEvidence         `json:"semantic_evidence,omitempty"`
	Context               contextpkg.CachedCompile   `json:"context"`
	Route                 router.Decision            `json:"route"`
	MechanicalOperations  int                        `json:"mechanical_operations"`
	PlannedLocalAICalls   int                        `json:"planned_local_ai_calls"`
	PlannedStrongAICalls  int                        `json:"planned_strong_ai_calls"`
	PreserveUserDirty     bool                       `json:"preserve_user_dirty"`
	Warnings              []string                   `json:"warnings,omitempty"`
}

type Engine struct { Service *core.Service }

func (e Engine) Prepare(ctx context.Context, sessionID, task string) (Plan, error) {
	if e.Service == nil { return Plan{}, fmt.Errorf("service is required") }
	if strings.TrimSpace(task)=="" { return Plan{}, fmt.Errorf("task is required") }
	if strings.TrimSpace(sessionID)=="" { return Plan{}, fmt.Errorf("session_id is required") }
	pm := projectmode.Engine{Root:e.Service.Config.Root,StateDir:e.Service.Config.StateDir}
	baseline, err := pm.Begin(ctx, sessionID, task); if err != nil { return Plan{}, err }
	impact, err := pm.Impact(ctx, sessionID); if err != nil { return Plan{}, err }

	mechanical := 8 // mode, git/head, dirty snapshot, language/build/test/CI discovery, ownership and impact ranking
	semanticEvidence := []SemanticEvidence{}
	if baseline.Project.Mode=="existing" && e.Service.Semantic != nil {
		for _, term := range semanticTerms(task, 4) {
			result, findErr := e.Service.Semantic.FindSymbol(ctx, domain.SymbolQuery{Pattern:term,Depth:1})
			mechanical++
			if findErr==nil && strings.TrimSpace(result.Raw)!="" { semanticEvidence=append(semanticEvidence,SemanticEvidence{Query:term,Result:result}) }
		}
	}

	view, err := hardware.Review(ctx,e.Service.Config.StateDir); if err != nil { return Plan{}, err }
	mechanical++
	compiled, err := (contextpkg.Compiler{Service:e.Service}).CompileCached(ctx,task,view.Effective.SoftContextTokens); if err != nil { return Plan{}, err }
	mechanical++
	model:=e.Service.Config.LocalModel;if view.Effective.PreferredModel!=""{model=view.Effective.PreferredModel}
	route, err := (router.Router{Stats:e.Service.Store}).Decide(ctx,router.Request{Task:task,TaskType:baseline.TaskKind,Model:model,ContextTokens:compiled.Packet.EstimatedTokens,ProjectMode:baseline.Project.Mode,DiscoveryReady:impact.DiscoveryReady});if err!=nil{return Plan{},err}
	mechanical++
	local,strong:=plannedAI(route)
	warnings:=[]string{}
	if len(baseline.UserDirty)>0 { warnings=append(warnings,"pre-existing developer changes are protected as USER_DIRTY and must not be reverted or overwritten implicitly") }
	if len(impact.Ownership.Conflicts)>0 { warnings=append(warnings,"files changed after baseline overlap USER_DIRTY; require explicit conflict-aware review before applying edits") }
	if baseline.Project.Mode=="existing" && len(semanticEvidence)==0 { warnings=append(warnings,"semantic evidence is unavailable or empty; continue with repo-map/context evidence and require stronger verification for broad changes") }
	return Plan{SessionID:sessionID,Task:task,Project:baseline.Project,Baseline:baseline,Impact:impact,Semantic:semanticEvidence,Context:compiled,Route:route,MechanicalOperations:mechanical,PlannedLocalAICalls:local,PlannedStrongAICalls:strong,PreserveUserDirty:true,Warnings:warnings},nil
}

func plannedAI(d router.Decision)(int,int){switch d.Route{case "mechanical":return 0,0;case "local":return 1,0;case "local-verify":return 1,1;default:return 0,1}}

func semanticTerms(task string, limit int) []string {
	stop:=map[string]bool{"the":true,"and":true,"for":true,"with":true,"add":true,"fix":true,"bug":true,"this":true,"that":true,"нужно":true,"сделать":true,"добавить":true,"починить":true,"фикс":true,"проект":true}
	seen:=map[string]bool{};out:=[]string{}
	for _,term:=range strings.FieldsFunc(strings.ToLower(task),func(r rune)bool{return !(r>='a'&&r<='z'||r>='0'&&r<='9'||r>='а'&&r<='я')}){if len([]rune(term))<3||stop[term]||seen[term]{continue};seen[term]=true;out=append(out,term);if len(out)>=limit{break}}
	return out
}
