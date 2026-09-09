package localworker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ulaista/usage-ai-on-dev/internal/domain"
	"github.com/ulaista/usage-ai-on-dev/internal/economy"
	"github.com/ulaista/usage-ai-on-dev/internal/hardware"
	"github.com/ulaista/usage-ai-on-dev/internal/router"
	"github.com/ulaista/usage-ai-on-dev/internal/store"
)

const workerPromptVersion = "bounded-worker-v4-brownfield"

var executionFlights economy.Group

type Evidence struct {
	Answer          string   `json:"answer"`
	Evidence        []string `json:"evidence,omitempty"`
	Risks           []string `json:"risks,omitempty"`
	AffectedSymbols []string `json:"affected_symbols,omitempty"`
	Verification    []string `json:"verification,omitempty"`
	Uncertainty     float64  `json:"uncertainty"`
}

type Request struct {
	Task           string `json:"task"`
	TaskType       string `json:"task_type,omitempty"`
	Context        string `json:"context,omitempty"`
	ContextTokens  int    `json:"context_tokens,omitempty"`
	ProjectMode    string `json:"project_mode,omitempty"`
	DiscoveryReady bool   `json:"discovery_ready,omitempty"`
}

type Result struct {
	ExecutionID         string            `json:"execution_id"`
	Model               string            `json:"model"`
	Hardware            hardware.Snapshot `json:"hardware"`
	EffectiveLimits     hardware.Limits   `json:"effective_limits"`
	Decision            hardware.Decision `json:"decision"`
	RouteDecision       router.Decision   `json:"route_decision"`
	Evidence            Evidence          `json:"evidence"`
	InputTokens         int               `json:"input_tokens"`
	OutputTokens        int               `json:"output_tokens"`
	LatencyMillis       int64             `json:"latency_ms"`
	CacheHit            bool              `json:"cache_hit"`
	SingleflightJoin    bool              `json:"singleflight_join"`
	TokensSaved         int               `json:"tokens_saved"`
	FallbackRequired    bool              `json:"fallback_required"`
	FallbackReason      string            `json:"fallback_reason,omitempty"`
	StrongReviewRequired bool             `json:"strong_review_required"`
}

type cachedExecution struct { Evidence Evidence `json:"evidence"`; InputTokens int `json:"input_tokens"`; OutputTokens int `json:"output_tokens"` }

type Worker struct {
	Model      string
	OllamaURL  string
	StateDir   string
	Detector   hardware.Detector
	Limits     *hardware.Limits
	Store      *store.Store
	HTTPClient *http.Client
	sem        chan struct{}
	once       sync.Once
}

func (w *Worker) init() {
	w.once.Do(func() {
		maxWorkers := 1
		if w.Limits != nil && w.Limits.MaxParallelWorkers > 0 { maxWorkers = w.Limits.MaxParallelWorkers }
		w.sem = make(chan struct{}, maxWorkers)
		if w.Detector == nil { w.Detector = hardware.SystemDetector{} }
		if w.HTTPClient == nil { w.HTTPClient = &http.Client{Timeout: 3 * time.Minute} }
		if w.OllamaURL == "" { w.OllamaURL = "http://127.0.0.1:11434" }
		if w.Model == "" { w.Model = "qwen3.5:4b" }
	})
}

func (w *Worker) Run(ctx context.Context, req Request) (Result, error) {
	w.init()
	if strings.TrimSpace(req.Task) == "" { return Result{}, fmt.Errorf("task is required") }
	snapshot, err := w.Detector.Snapshot(ctx); if err != nil { return Result{}, err }
	limits := hardware.LimitsFromProfile(snapshot.Profile); profile := snapshot.Profile
	if w.Limits != nil { limits = *w.Limits; profile = hardware.ApplyLimitsToProfile(profile, limits) }
	routeDecision, err := (router.Router{Stats:w.Store}).Decide(ctx, router.Request{Task:req.Task,TaskType:req.TaskType,Model:w.Model,ContextTokens:req.ContextTokens,ProjectMode:req.ProjectMode,DiscoveryReady:req.DiscoveryReady})
	if err != nil { return Result{}, err }
	req.TaskType = routeDecision.TaskType
	if routeDecision.RecommendedContext > 0 && routeDecision.RecommendedContext < profile.HardContextTokens { profile.HardContextTokens = routeDecision.RecommendedContext }
	if profile.SoftContextTokens > profile.HardContextTokens { profile.SoftContextTokens = profile.HardContextTokens }
	decision := profile.Evaluate(snapshot.Resources, req.ContextTokens)
	base := Result{ExecutionID:fmt.Sprintf("EXE-%d",time.Now().UnixNano()),Model:w.Model,Hardware:snapshot,EffectiveLimits:limits,Decision:decision,RouteDecision:routeDecision,StrongReviewRequired:routeDecision.RequireStrongReview}
	if routeDecision.Route == "strong" { base.FallbackRequired=true;base.FallbackReason=routeDecision.Reason;_ = w.record(ctx,req,base,base.FallbackReason);return base,nil }
	if routeDecision.NoAI { base.FallbackRequired=true;base.FallbackReason="adaptive router selected a no-AI mechanical path; resolve through cache/semantic tools instead of local inference";_ = w.record(ctx,req,base,base.FallbackReason);return base,nil }
	if !decision.Allowed { base.FallbackRequired=true;base.FallbackReason=decision.Reason;_ = w.record(ctx,req,base,decision.Reason);return base,nil }
	if req.ContextTokens > decision.RecommendedContextTokens && decision.RecommendedContextTokens > 0 { base.FallbackRequired=true;base.FallbackReason="context must be recompiled to the recommended local budget before execution";_ = w.record(ctx,req,base,base.FallbackReason);return base,nil }
	key:=economy.Fingerprint(workerPromptVersion,economy.NormalizeTask(req.Task),req.TaskType,w.Model,snapshot.Inventory.HardwareID,req.Context,strconv.Itoa(profile.HardContextTokens),strconv.Itoa(profile.MaxOutputTokens),routeDecision.Route,req.ProjectMode,strconv.FormatBool(req.DiscoveryReady))
	cache:=economy.FileCache{Dir:filepath.Join(w.StateDir,"cache")};var cached cachedExecution
	if w.StateDir!="" { if hit,e:=cache.Load("executions",key,&cached);e==nil&&hit { return w.cachedResult(ctx,req,base,key,cached,"execution",false),nil } }
	value,runErr,joined:=executionFlights.Do(key,func()(any,error){
		if w.StateDir!="" { var second cachedExecution; if hit,e:=cache.Load("executions",key,&second);e==nil&&hit{return second,nil} }
		select { case w.sem<-struct{}{}: defer func(){<-w.sem}(); case <-ctx.Done(): return nil,ctx.Err() }
		start:=time.Now(); response,e:=w.callOllama(ctx,req,profile); if e!=nil{return nil,e}; payload:=cachedExecution{Evidence:response.Evidence,InputTokens:response.PromptTokens,OutputTokens:response.OutputTokens}; base.LatencyMillis=time.Since(start).Milliseconds(); if w.StateDir!=""{_ = cache.Save("executions",key,payload)}; return payload,nil
	})
	if runErr!=nil { base.FallbackRequired=true;base.FallbackReason=runErr.Error();_ = w.record(ctx,req,base,runErr.Error());return base,nil }
	payload:=value.(cachedExecution); if joined { return w.cachedResult(ctx,req,base,key,payload,"singleflight",true),nil }
	base.Evidence=payload.Evidence;base.InputTokens=payload.InputTokens;base.OutputTokens=payload.OutputTokens;_ = w.record(ctx,req,base,"");return base,nil
}

func (w *Worker) cachedResult(ctx context.Context,req Request,base Result,key string,cached cachedExecution,kind string,joined bool)Result{base.Evidence=cached.Evidence;base.CacheHit=!joined;base.SingleflightJoin=joined;base.TokensSaved=cached.InputTokens+cached.OutputTokens;if w.Store!=nil{_ = w.Store.RecordSaving(ctx,domain.CacheSaving{Kind:kind,Key:key,SavedInputTokens:cached.InputTokens,SavedOutputTokens:cached.OutputTokens,CreatedAt:time.Now().UTC()})};_ = w.record(ctx,req,base,"");return base}
type ollamaRequest struct{Model string `json:"model"`;Stream bool `json:"stream"`;Format string `json:"format"`;Messages []ollamaMessage `json:"messages"`;Options map[string]any `json:"options,omitempty"`;KeepAlive string `json:"keep_alive,omitempty"`}
type ollamaMessage struct{Role string `json:"role"`;Content string `json:"content"`}
type ollamaResponse struct{Message struct{Content string `json:"content"`} `json:"message"`;PromptEvalCount int `json:"prompt_eval_count"`;EvalCount int `json:"eval_count"`}
type workerResponse struct{Evidence Evidence;PromptTokens int;OutputTokens int}
func (w *Worker) callOllama(ctx context.Context,req Request,profile hardware.Profile)(workerResponse,error){system:=`You are Project Brain's bounded local worker. Do only the requested low-risk task. For an existing project, preserve developer-owned dirty changes and use only the supplied recovery/context evidence. Do not make architecture, security, destructive migration, breaking API, or production-risk decisions. Return JSON with answer, evidence, risks, affected_symbols, verification, uncertainty. uncertainty is 0 to 1.`;user:="TASK:\n"+req.Task;if req.Context!=""{user+="\n\nBOUNDED CONTEXT:\n"+req.Context};numCtx:=profile.HardContextTokens;if req.ContextTokens>0{needed:=req.ContextTokens+profile.MaxOutputTokens+512;if needed<2048{needed=2048};if needed<numCtx{numCtx=needed}};payload:=ollamaRequest{Model:w.Model,Stream:false,Format:"json",KeepAlive:"2m",Messages:[]ollamaMessage{{Role:"system",Content:system},{Role:"user",Content:user}},Options:map[string]any{"temperature":0,"num_ctx":numCtx,"num_predict":profile.MaxOutputTokens}};data,_:=json.Marshal(payload);httpReq,err:=http.NewRequestWithContext(ctx,http.MethodPost,strings.TrimRight(w.OllamaURL,"/")+"/api/chat",bytes.NewReader(data));if err!=nil{return workerResponse{},err};httpReq.Header.Set("Content-Type","application/json");resp,err:=w.HTTPClient.Do(httpReq);if err!=nil{return workerResponse{},fmt.Errorf("ollama unavailable: %w",err)};defer resp.Body.Close();body,_:=io.ReadAll(io.LimitReader(resp.Body,2<<20));if resp.StatusCode<200||resp.StatusCode>=300{return workerResponse{},fmt.Errorf("ollama returned %s: %s",resp.Status,strings.TrimSpace(string(body)))};var parsed ollamaResponse;if err:=json.Unmarshal(body,&parsed);err!=nil{return workerResponse{},fmt.Errorf("decode ollama response: %w",err)};var evidence Evidence;if err:=json.Unmarshal([]byte(parsed.Message.Content),&evidence);err!=nil{return workerResponse{},fmt.Errorf("local worker returned invalid evidence JSON: %w",err)};if evidence.Uncertainty<0||evidence.Uncertainty>1{return workerResponse{},fmt.Errorf("local worker uncertainty must be between 0 and 1")};return workerResponse{Evidence:evidence,PromptTokens:parsed.PromptEvalCount,OutputTokens:parsed.EvalCount},nil}
func (w *Worker) record(ctx context.Context,req Request,result Result,errText string)error{if w.Store==nil{return nil};route:=result.RouteDecision.Route;if route==""{route="local"};if result.CacheHit{route="local-cache"};if result.SingleflightJoin{route="local-singleflight"};return w.Store.RecordExecution(ctx,domain.Execution{ID:result.ExecutionID,Task:req.Task,TaskType:req.TaskType,Model:result.Model,Route:route,LatencyMillis:result.LatencyMillis,InputTokens:result.InputTokens,OutputTokens:result.OutputTokens,Fallback:result.FallbackRequired,Error:errText,CreatedAt:time.Now().UTC()})}
