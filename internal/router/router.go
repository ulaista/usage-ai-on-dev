package router

import (
	"context"
	"math"
	"strings"

	"github.com/ulaista/usage-ai-on-dev/internal/domain"
)

type StatsProvider interface { WorkloadStats(context.Context, string, string) (domain.WorkloadStats, error) }

type Request struct {
	Task          string `json:"task"`
	TaskType      string `json:"task_type,omitempty"`
	Model         string `json:"model,omitempty"`
	ContextTokens int    `json:"context_tokens,omitempty"`
	CacheHit      bool   `json:"cache_hit,omitempty"`
	SemanticOnly  bool   `json:"semantic_only,omitempty"`
	ProjectMode   string `json:"project_mode,omitempty"`
	DiscoveryReady bool  `json:"discovery_ready,omitempty"`
}

type Decision struct {
	Route                string               `json:"route"`
	TaskType             string               `json:"task_type"`
	Risk                 string               `json:"risk"`
	Reason               string               `json:"reason"`
	RequireStrongReview  bool                 `json:"require_strong_review"`
	RecommendedContext   int                  `json:"recommended_context_tokens,omitempty"`
	LocalConfidence      float64              `json:"local_confidence"`
	Historical           domain.WorkloadStats `json:"historical"`
	HardGate              bool                 `json:"hard_gate"`
	NoAI                  bool                 `json:"no_ai"`
}

type Router struct { Stats StatsProvider }

func Classify(task, explicit string) string {
	if strings.TrimSpace(explicit) != "" && normalizeType(explicit)!="bounded" { return normalizeType(explicit) }
	t := strings.ToLower(task)
	switch {
	case containsAny(t, "security", "auth", "authentication", "authorization", "crypto", "permission", "secret", "token validation", "jwt"):
		return "security"
	case containsAny(t, "migration", "schema", "database migration", "drop table", "backfill"):
		return "migration"
	case containsAny(t, "breaking api", "public api", "backward compatibility", "breaking change"):
		return "breaking-api"
	case containsAny(t, "production incident", "outage", "rollback", "production bug", "sev1", "sev2"):
		return "production"
	case containsAny(t, "architecture", "architect", "redesign", "system design"):
		return "architecture"
	case containsAny(t, "test", "tests", "coverage", "тест"):
		return "tests"
	case containsAny(t, "docs", "documentation", "readme", "comment", "документац"):
		return "docs"
	case containsAny(t, "refactor", "rename", "cleanup", "simplify", "рефактор"):
		return "refactor"
	case containsAny(t, "bug", "fix", "repair", "ошиб", "баг", "почин", "фикс"):
		return "bugfix"
	default:
		return "implementation"
	}
}

func (r Router) Decide(ctx context.Context, req Request) (Decision, error) {
	typeName := Classify(req.Task, req.TaskType)
	if req.CacheHit || req.SemanticOnly { return Decision{Route:"mechanical",TaskType:typeName,Risk:"low",Reason:"request can be answered from deterministic cache/semantic state",NoAI:true,LocalConfidence:1}, nil }
	if req.ProjectMode=="existing" && !req.DiscoveryReady {
		return Decision{Route:"strong",TaskType:typeName,Risk:"medium",Reason:"existing project has not satisfied the required discovery contract before modification",RequireStrongReview:true,HardGate:true},nil
	}
	if risk, reason, hard := hardRisk(req.Task, typeName); hard { return Decision{Route:"strong",TaskType:typeName,Risk:risk,Reason:reason,RequireStrongReview:true,HardGate:true}, nil }

	stats := domain.WorkloadStats{TaskType:typeName, Model:req.Model}
	if r.Stats != nil { if measured, err := r.Stats.WorkloadStats(ctx, typeName, req.Model); err == nil { stats = measured } }
	confidence := confidence(stats)
	contextBudget := recommendedContext(typeName, req.ContextTokens, confidence)
	if stats.Samples < 5 || stats.Reviewed < 3 { return Decision{Route:"local-verify",TaskType:typeName,Risk:"low",Reason:"insufficient workload history; use local execution with strong verification",RequireStrongReview:true,RecommendedContext:contextBudget,LocalConfidence:confidence,Historical:stats}, nil }
	if stats.FallbackRate >= .25 || stats.ErrorRate >= .15 || stats.AcceptanceRate < .70 { return Decision{Route:"strong",TaskType:typeName,Risk:"medium",Reason:"historical local reliability is below routing threshold",RequireStrongReview:true,RecommendedContext:contextBudget,LocalConfidence:confidence,Historical:stats}, nil }
	if stats.Reviewed >= 10 && stats.AcceptanceRate >= .95 && stats.FallbackRate <= .05 && stats.ErrorRate <= .03 { return Decision{Route:"local",TaskType:typeName,Risk:"low",Reason:"workload has strong local acceptance history and low fallback/error rates",RequireStrongReview:false,RecommendedContext:contextBudget,LocalConfidence:confidence,Historical:stats}, nil }
	return Decision{Route:"local-verify",TaskType:typeName,Risk:"low",Reason:"local workload is healthy but still requires strong spot verification",RequireStrongReview:true,RecommendedContext:contextBudget,LocalConfidence:confidence,Historical:stats}, nil
}

func hardRisk(task, typ string) (string,string,bool) {
	t := strings.ToLower(task)
	switch typ { case "security": return "high","security/auth/crypto/permission changes always require strong-model ownership",true; case "migration": return "high","database/data migrations require strong-model ownership",true; case "breaking-api": return "high","breaking public API changes require strong-model ownership",true; case "production": return "high","production incidents and rollout decisions require strong-model ownership",true; case "architecture": return "high","architecture and fundamental dependency decisions require strong-model ownership",true }
	if containsAny(t,"delete production","destructive","rotate secrets","privilege escalation") { return "high","deterministic high-risk phrase matched",true }
	return "low","",false
}
func confidence(s domain.WorkloadStats) float64 { if s.Samples==0{return .5};accept:=s.AcceptanceRate;if s.Reviewed==0{accept=.5};sampleWeight:=math.Min(1,float64(s.Reviewed)/20);reliability:=accept*(1-s.FallbackRate)*(1-s.ErrorRate);return clamp(.5*(1-sampleWeight)+reliability*sampleWeight,0,1) }
func recommendedContext(taskType string,requested int,confidence float64)int{base:=6000;switch taskType{case "docs","tests":base=4000;case "refactor","bugfix":base=6000;default:base=8000};if confidence>=.9{base=int(float64(base)*.75)};if requested>0&&requested<base{return requested};return base}
func normalizeType(v string)string{return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(v),"_","-"))}
func containsAny(s string,values ...string)bool{for _,v:=range values{if strings.Contains(s,v){return true}};return false}
func clamp(v,lo,hi float64)float64{if v<lo{return lo};if v>hi{return hi};return v}
