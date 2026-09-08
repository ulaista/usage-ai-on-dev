package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ulaista/usage-ai-on-dev/internal/autotune"
	contextpkg "github.com/ulaista/usage-ai-on-dev/internal/context"
	"github.com/ulaista/usage-ai-on-dev/internal/core"
	"github.com/ulaista/usage-ai-on-dev/internal/domain"
	"github.com/ulaista/usage-ai-on-dev/internal/hardware"
	"github.com/ulaista/usage-ai-on-dev/internal/localworker"
	"github.com/ulaista/usage-ai-on-dev/internal/repomap"
)

func text(v any) (*mcp.CallToolResult, any, error) {
	data,err:=json.MarshalIndent(v,"","  "); if err!=nil{return nil,nil,err}
	return &mcp.CallToolResult{Content:[]mcp.Content{&mcp.TextContent{Text:string(data)}}},v,nil
}

func New(svc *core.Service) *mcp.Server {
	server:=mcp.NewServer(&mcp.Implementation{Name:"project-brain",Version:"v0.5.0"},nil)
	type emptyArgs struct{}
	mcp.AddTool(server,&mcp.Tool{Name:"brain_status",Description:"Return Project Brain status, active intent count, storage, hardware policy and latest autotune recommendation."},func(ctx context.Context,_ *mcp.CallToolRequest,_ emptyArgs)(*mcp.CallToolResult,any,error){
		status,err:=svc.Status(ctx); if err!=nil{return nil,nil,err}; if view,err:=hardware.Review(ctx,svc.Config.StateDir);err==nil{status["hardware_policy"]=view}; if report,err:=autotune.LoadReport(autotune.ReportPath(svc.Config.StateDir));err==nil{status["autotune_recommendation"]=report.Recommendation}; return text(status)
	})
	mcp.AddTool(server,&mcp.Tool{Name:"brain_hardware",Description:"Detect CPU, RAM, GPU/VRAM where available, runtime memory pressure and show recommended/effective local-AI limits plus whether user acceptance is required."},func(ctx context.Context,_ *mcp.CallToolRequest,_ emptyArgs)(*mcp.CallToolResult,any,error){
		view,err:=hardware.Review(ctx,svc.Config.StateDir); if err!=nil{return nil,nil,err}; return text(view)
	})
	type acceptHardwareArgs struct{ MemoryReserveMB int `json:"memory_reserve_mb,omitempty"`; SoftContextTokens int `json:"soft_context_tokens,omitempty"`; HardContextTokens int `json:"hard_context_tokens,omitempty"`; MaxOutputTokens int `json:"max_output_tokens,omitempty"`; MaxParallelWorkers int `json:"max_parallel_workers,omitempty"`; PreferredModel string `json:"preferred_model,omitempty"`; PreferredModelClass string `json:"preferred_model_class,omitempty"` }
	mcp.AddTool(server,&mcp.Tool{Name:"brain_hardware_accept",Description:"Accept current detected-hardware recommendations, optionally with user overrides. Overrides are persisted and only apply to the current hardware ID."},func(ctx context.Context,_ *mcp.CallToolRequest,args acceptHardwareArgs)(*mcp.CallToolResult,any,error){
		view,err:=hardware.Review(ctx,svc.Config.StateDir); if err!=nil{return nil,nil,err}
		override:=hardware.Limits{MemoryReserveMB:args.MemoryReserveMB,SoftContextTokens:args.SoftContextTokens,HardContextTokens:args.HardContextTokens,MaxOutputTokens:args.MaxOutputTokens,MaxParallelWorkers:args.MaxParallelWorkers,PreferredModel:args.PreferredModel,PreferredModelClass:args.PreferredModelClass}
		policy:=hardware.AcceptPolicy(view.Snapshot,override); if err:=hardware.SavePolicy(hardware.PolicyPath(svc.Config.StateDir),policy);err!=nil{return nil,nil,err}; return text(hardware.BuildView(view.Snapshot,policy))
	})
	mcp.AddTool(server,&mcp.Tool{Name:"brain_hardware_reset",Description:"Discard accepted hardware overrides and return to automatically recommended limits."},func(ctx context.Context,_ *mcp.CallToolRequest,_ emptyArgs)(*mcp.CallToolResult,any,error){
		if err:=hardware.ResetPolicy(hardware.PolicyPath(svc.Config.StateDir));err!=nil{return nil,nil,err}; view,err:=hardware.Review(ctx,svc.Config.StateDir);if err!=nil{return nil,nil,err};return text(view)
	})
	type autotuneArgs struct{ Models []string `json:"models,omitempty"`; Contexts []int `json:"contexts,omitempty"`; MaxModels int `json:"max_models,omitempty"`; OutputTokens int `json:"output_tokens,omitempty"` }
	mcp.AddTool(server,&mcp.Tool{Name:"brain_autotune",Description:"Benchmark installed Ollama models on the current machine, measure token throughput and resource pressure, save a recommendation, and do not apply it until the user accepts it."},func(ctx context.Context,_ *mcp.CallToolRequest,args autotuneArgs)(*mcp.CallToolResult,any,error){
		runner:=autotune.Runner{OllamaURL:svc.Config.OllamaURL,StateDir:svc.Config.StateDir}; report,err:=runner.Run(ctx,autotune.Options{Models:args.Models,Contexts:args.Contexts,MaxModels:args.MaxModels,OutputTokens:args.OutputTokens});if err!=nil{return nil,nil,err};if err:=autotune.SaveReport(autotune.ReportPath(svc.Config.StateDir),report);err!=nil{return nil,nil,err};return text(map[string]any{"applied":false,"requires_user_acceptance":report.Recommendation.PreferredModel!="","report":report})
	})
	mcp.AddTool(server,&mcp.Tool{Name:"brain_autotune_show",Description:"Return the latest saved capability benchmark and recommendation without changing policy."},func(ctx context.Context,_ *mcp.CallToolRequest,_ emptyArgs)(*mcp.CallToolResult,any,error){report,err:=autotune.LoadReport(autotune.ReportPath(svc.Config.StateDir));if err!=nil{return nil,nil,err};return text(report)})
	mcp.AddTool(server,&mcp.Tool{Name:"brain_autotune_accept",Description:"Accept the latest measured autotune recommendation for the current hardware, optionally with user overrides."},func(ctx context.Context,_ *mcp.CallToolRequest,args acceptHardwareArgs)(*mcp.CallToolResult,any,error){
		report,err:=autotune.LoadReport(autotune.ReportPath(svc.Config.StateDir));if err!=nil{return nil,nil,err};view,err:=hardware.Review(ctx,svc.Config.StateDir);if err!=nil{return nil,nil,err};if err:=autotune.ValidateReportForHardware(report,view.Snapshot.Inventory.HardwareID);err!=nil{return nil,nil,err}
		override:=report.Recommendation.PolicyOverride;user:=hardware.Limits{MemoryReserveMB:args.MemoryReserveMB,SoftContextTokens:args.SoftContextTokens,HardContextTokens:args.HardContextTokens,MaxOutputTokens:args.MaxOutputTokens,MaxParallelWorkers:args.MaxParallelWorkers,PreferredModel:args.PreferredModel,PreferredModelClass:args.PreferredModelClass};override=hardware.ApplyOverride(override,user)
		policy:=hardware.AcceptPolicy(view.Snapshot,override);if err:=hardware.SavePolicy(hardware.PolicyPath(svc.Config.StateDir),policy);err!=nil{return nil,nil,err};return text(map[string]any{"applied":true,"source":"autotune","hardware_policy":hardware.BuildView(view.Snapshot,policy),"recommendation":report.Recommendation})
	})
	type contextArgs struct{ Task string `json:"task"`; MaxTokens int `json:"max_tokens,omitempty"` }
	mcp.AddTool(server,&mcp.Tool{Name:"brain_context",Description:"Compile task-specific context from Git state, active intents, ranked repository graph and Serena semantic evidence under a token budget."},func(ctx context.Context,_ *mcp.CallToolRequest,args contextArgs)(*mcp.CallToolResult,any,error){
		if args.Task==""{return nil,nil,fmt.Errorf("task is required")}; packet,err:=(contextpkg.Compiler{Service:svc}).Compile(ctx,args.Task,args.MaxTokens);if err!=nil{return nil,nil,err};return &mcp.CallToolResult{Content:[]mcp.Content{&mcp.TextContent{Text:packet.RenderMarkdown()}}},packet,nil
	})
	type workerArgs struct{ Task string `json:"task"`; TaskType string `json:"task_type,omitempty"`; Context string `json:"context,omitempty"`; ContextTokens int `json:"context_tokens,omitempty"`; Compile bool `json:"compile_context,omitempty"` }
	mcp.AddTool(server,&mcp.Tool{Name:"brain_local_run",Description:"Run a bounded local task using the effective user-approved hardware policy. Returns typed evidence or a structured strong-model fallback reason."},func(ctx context.Context,_ *mcp.CallToolRequest,args workerArgs)(*mcp.CallToolResult,any,error){
		if args.Task==""{return nil,nil,fmt.Errorf("task is required")}; view,err:=hardware.Review(ctx,svc.Config.StateDir);if err!=nil{return nil,nil,err}
		if args.Compile||args.Context==""{packet,err:=(contextpkg.Compiler{Service:svc}).Compile(ctx,args.Task,view.Effective.SoftContextTokens);if err!=nil{return nil,nil,err};args.Context=packet.RenderMarkdown();args.ContextTokens=packet.EstimatedTokens}
		model:=svc.Config.LocalModel;if view.Effective.PreferredModel!=""{model=view.Effective.PreferredModel};worker:=localworker.Worker{Model:model,OllamaURL:svc.Config.OllamaURL,Store:svc.Store,Limits:&view.Effective};result,err:=worker.Run(ctx,localworker.Request{Task:args.Task,TaskType:args.TaskType,Context:args.Context,ContextTokens:args.ContextTokens});if err!=nil{return nil,nil,err};return text(map[string]any{"hardware_policy":view,"result":result})
	})
	type repoMapArgs struct{ Task string `json:"task"`; MaxTokens int `json:"max_tokens,omitempty"`; Changed []string `json:"changed_files,omitempty"` }
	mcp.AddTool(server,&mcp.Tool{Name:"brain_repo_map",Description:"Build a compact task-ranked repository map from dependency edges, Git changes, semantic seeds and code signatures."},func(ctx context.Context,_ *mcp.CallToolRequest,args repoMapArgs)(*mcp.CallToolResult,any,error){
		if args.Task==""{return nil,nil,fmt.Errorf("task is required")};budget:=args.MaxTokens;if budget<=0{budget=svc.Config.RepoMapTokens};result,err:=(repomap.Builder{Root:svc.Config.Root,CachePath:filepath.Join(svc.Config.StateDir,"cache","repomap.json"),MaxFiles:svc.Config.RepoMapMaxFiles}).Build(ctx,repomap.Request{Task:args.Task,ChangedFiles:args.Changed,TokenBudget:budget});if err!=nil{return nil,nil,err};return &mcp.CallToolResult{Content:[]mcp.Content{&mcp.TextContent{Text:result.RenderMarkdown()}}},result,nil
	})
	type createIntentArgs struct{ Title string `json:"title" jsonschema:"short intent title"`; Goal string `json:"goal,omitempty" jsonschema:"desired end state"`; Constraints []string `json:"constraints,omitempty"`; Affected []string `json:"affected,omitempty"`; Verification []string `json:"verification,omitempty"` }
	mcp.AddTool(server,&mcp.Tool{Name:"brain_intent_create",Description:"Create durable development intent before multi-step work."},func(ctx context.Context,_ *mcp.CallToolRequest,args createIntentArgs)(*mcp.CallToolResult,any,error){if args.Title==""{return nil,nil,fmt.Errorf("title is required")};in,err:=svc.CreateIntent(ctx,args.Title,args.Goal,args.Constraints,args.Affected,args.Verification);if err!=nil{return nil,nil,err};return text(in)})
	type batchArgs struct{ IntentID string `json:"intent_id"`; Title string `json:"title"`; Scope []string `json:"scope,omitempty"` }
	mcp.AddTool(server,&mcp.Tool{Name:"brain_batch_create",Description:"Create a semantic change batch for an active intent."},func(ctx context.Context,_ *mcp.CallToolRequest,args batchArgs)(*mcp.CallToolResult,any,error){if args.IntentID==""||args.Title==""{return nil,nil,fmt.Errorf("intent_id and title are required")};b,err:=svc.CreateBatch(ctx,args.IntentID,args.Title,args.Scope);if err!=nil{return nil,nil,err};return text(b)})
	mcp.AddTool(server,&mcp.Tool{Name:"brain_intent_list",Description:"List active durable development intents."},func(ctx context.Context,_ *mcp.CallToolRequest,_ emptyArgs)(*mcp.CallToolResult,any,error){rows,err:=svc.Store.ListActiveIntents(ctx);if err!=nil{return nil,nil,err};return text(rows)})
	type telemetryArgs struct{ Model string `json:"model,omitempty"` }
	mcp.AddTool(server,&mcp.Tool{Name:"brain_telemetry",Description:"Summarize measured model latency, tokens, fallbacks and strong-model acceptance."},func(ctx context.Context,_ *mcp.CallToolRequest,args telemetryArgs)(*mcp.CallToolResult,any,error){stats,err:=svc.Store.TelemetrySummary(ctx,args.Model);if err!=nil{return nil,nil,err};return text(stats)})
	type symbolArgs struct{ Pattern string `json:"pattern"`; RelativePath string `json:"relative_path,omitempty"`; IncludeBody bool `json:"include_body,omitempty"`; Depth int `json:"depth,omitempty"` }
	mcp.AddTool(server,&mcp.Tool{Name:"brain_find_symbol",Description:"Find code symbols through the configured semantic provider."},func(ctx context.Context,_ *mcp.CallToolRequest,args symbolArgs)(*mcp.CallToolResult,any,error){if svc.Semantic==nil{return nil,nil,fmt.Errorf("semantic provider is not connected: %s",svc.SemanticError)};result,err:=svc.Semantic.FindSymbol(ctx,domain.SymbolQuery{Pattern:args.Pattern,RelativePath:args.RelativePath,IncludeBody:args.IncludeBody,Depth:args.Depth});if err!=nil{return nil,nil,err};return text(result)})
	type refsArgs struct{ NamePath string `json:"name_path"`; RelativePath string `json:"relative_path"` }
	mcp.AddTool(server,&mcp.Tool{Name:"brain_find_references",Description:"Find references to a symbol using the semantic provider instead of scanning the whole repository."},func(ctx context.Context,_ *mcp.CallToolRequest,args refsArgs)(*mcp.CallToolResult,any,error){if svc.Semantic==nil{return nil,nil,fmt.Errorf("semantic provider is not connected: %s",svc.SemanticError)};result,err:=svc.Semantic.FindReferences(ctx,args.NamePath,args.RelativePath);if err!=nil{return nil,nil,err};return text(result)})
	return server
}
