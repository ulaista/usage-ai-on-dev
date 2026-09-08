package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ulaista/usage-ai-on-dev/internal/autotune"
	"github.com/ulaista/usage-ai-on-dev/internal/config"
	contextpkg "github.com/ulaista/usage-ai-on-dev/internal/context"
	"github.com/ulaista/usage-ai-on-dev/internal/core"
	"github.com/ulaista/usage-ai-on-dev/internal/domain"
	"github.com/ulaista/usage-ai-on-dev/internal/hardware"
	"github.com/ulaista/usage-ai-on-dev/internal/localworker"
	"github.com/ulaista/usage-ai-on-dev/internal/mcpserver"
	"github.com/ulaista/usage-ai-on-dev/internal/repomap"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: brain-core [--root PATH] <init|status|hardware|hardware-accept|hardware-reset|autotune|autotune-show|autotune-accept|context|repo-map|local-run|mcp|semantic-find|telemetry>")
}
func printJSON(v any) { data,_:=json.MarshalIndent(v,"","  "); fmt.Println(string(data)) }

func main() {
	root:=flag.String("root",".","project root"); flag.Parse()
	if flag.NArg()<1 { usage(); os.Exit(2) }
	abs,err:=filepath.Abs(*root); if err!=nil { log.Fatal(err) }
	cfg,err:=config.Load(abs); if err!=nil { log.Fatal(err) }
	ctx:=context.Background()

	switch flag.Arg(0) {
	case "init":
		if err:=cfg.Save(); err!=nil { log.Fatal(err) }
		svc,err:=core.Open(ctx,cfg,false); if err!=nil { log.Fatal(err) }; defer svc.Close()
		view, _ := hardware.Review(ctx,cfg.StateDir)
		printJSON(map[string]any{"state_dir":cfg.StateDir,"database":cfg.DatabasePath,"semantic_provider":cfg.SemanticProvider,"hardware":view})
	case "status":
		svc,err:=core.Open(ctx,cfg,false); if err!=nil { log.Fatal(err) }; defer svc.Close()
		status,err:=svc.Status(ctx); if err!=nil { log.Fatal(err) }
		if view,err:=hardware.Review(ctx,cfg.StateDir); err==nil { status["hardware_policy"] = view }
		if report,err:=autotune.LoadReport(autotune.ReportPath(cfg.StateDir)); err==nil { status["autotune_recommendation"] = report.Recommendation }
		printJSON(status)
	case "hardware":
		view,err:=hardware.Review(ctx,cfg.StateDir); if err!=nil { log.Fatal(err) }; printJSON(view)
	case "hardware-accept":
		view,err:=hardware.Review(ctx,cfg.StateDir); if err!=nil { log.Fatal(err) }
		var override hardware.Limits
		if flag.NArg()>1 {
			if err:=json.Unmarshal([]byte(flag.Arg(1)),&override); err!=nil { log.Fatalf("invalid override JSON: %v",err) }
		}
		policy:=hardware.AcceptPolicy(view.Snapshot,override)
		if err:=hardware.SavePolicy(hardware.PolicyPath(cfg.StateDir),policy); err!=nil { log.Fatal(err) }
		printJSON(hardware.BuildView(view.Snapshot,policy))
	case "hardware-reset":
		if err:=hardware.ResetPolicy(hardware.PolicyPath(cfg.StateDir)); err!=nil { log.Fatal(err) }
		view,err:=hardware.Review(ctx,cfg.StateDir); if err!=nil { log.Fatal(err) }; printJSON(view)
	case "autotune":
		var models []string
		if flag.NArg()>1 && strings.TrimSpace(flag.Arg(1))!="" { for _,name:=range strings.Split(flag.Arg(1),",") { if v:=strings.TrimSpace(name); v!="" { models=append(models,v) } } }
		runner:=autotune.Runner{OllamaURL:cfg.OllamaURL,StateDir:cfg.StateDir}
		report,err:=runner.Run(ctx,autotune.Options{Models:models,MaxModels:4,OutputTokens:64}); if err!=nil { log.Fatal(err) }
		if err:=autotune.SaveReport(autotune.ReportPath(cfg.StateDir),report); err!=nil { log.Fatal(err) }
		printJSON(map[string]any{"applied":false,"requires_user_acceptance":report.Recommendation.PreferredModel!="","report":report})
	case "autotune-show":
		report,err:=autotune.LoadReport(autotune.ReportPath(cfg.StateDir)); if err!=nil { log.Fatal(err) }; printJSON(report)
	case "autotune-accept":
		report,err:=autotune.LoadReport(autotune.ReportPath(cfg.StateDir)); if err!=nil { log.Fatal(err) }
		view,err:=hardware.Review(ctx,cfg.StateDir); if err!=nil { log.Fatal(err) }
		if err:=autotune.ValidateReportForHardware(report,view.Snapshot.Inventory.HardwareID); err!=nil { log.Fatal(err) }
		override:=report.Recommendation.PolicyOverride
		if flag.NArg()>1 {
			var userOverride hardware.Limits
			if err:=json.Unmarshal([]byte(flag.Arg(1)),&userOverride); err!=nil { log.Fatalf("invalid override JSON: %v",err) }
			override=hardware.ApplyOverride(override,userOverride)
		}
		policy:=hardware.AcceptPolicy(view.Snapshot,override)
		if err:=hardware.SavePolicy(hardware.PolicyPath(cfg.StateDir),policy); err!=nil { log.Fatal(err) }
		printJSON(map[string]any{"applied":true,"source":"autotune","hardware_policy":hardware.BuildView(view.Snapshot,policy),"recommendation":report.Recommendation})
	case "context":
		if flag.NArg()<2 { log.Fatal("context requires a task") }
		svc,err:=core.Open(ctx,cfg,true); if err!=nil { log.Fatal(err) }; defer svc.Close()
		packet,err:=(contextpkg.Compiler{Service:svc}).Compile(ctx,flag.Arg(1),cfg.TargetContext); if err!=nil { log.Fatal(err) }; fmt.Print(packet.RenderMarkdown())
	case "repo-map":
		if flag.NArg()<2 { log.Fatal("repo-map requires a task") }
		result,err:=(repomap.Builder{Root:cfg.Root,CachePath:filepath.Join(cfg.StateDir,"cache","repomap.json"),MaxFiles:cfg.RepoMapMaxFiles}).Build(ctx,repomap.Request{Task:flag.Arg(1),TokenBudget:cfg.RepoMapTokens}); if err!=nil { log.Fatal(err) }; fmt.Print(result.RenderMarkdown())
	case "local-run":
		if flag.NArg()<2 { log.Fatal("local-run requires a task") }
		svc,err:=core.Open(ctx,cfg,false); if err!=nil { log.Fatal(err) }; defer svc.Close()
		view,err:=hardware.Review(ctx,cfg.StateDir); if err!=nil { log.Fatal(err) }
		packet,err:=(contextpkg.Compiler{Service:svc}).Compile(ctx,flag.Arg(1),view.Effective.SoftContextTokens); if err!=nil { log.Fatal(err) }
		model:=cfg.LocalModel; if view.Effective.PreferredModel!="" { model=view.Effective.PreferredModel }
		worker:=localworker.Worker{Model:model,OllamaURL:cfg.OllamaURL,Store:svc.Store,Limits:&view.Effective}
		result,err:=worker.Run(ctx,localworker.Request{Task:flag.Arg(1),TaskType:"bounded",Context:packet.RenderMarkdown(),ContextTokens:packet.EstimatedTokens}); if err!=nil { log.Fatal(err) }
		printJSON(map[string]any{"hardware_policy":view,"result":result})
	case "telemetry":
		svc,err:=core.Open(ctx,cfg,false); if err!=nil { log.Fatal(err) }; defer svc.Close(); model:=""; if flag.NArg()>1 { model=flag.Arg(1) }
		stats,err:=svc.Store.TelemetrySummary(ctx,model); if err!=nil { log.Fatal(err) }; printJSON(stats)
	case "semantic-find":
		if flag.NArg()<2 { log.Fatal("semantic-find requires a symbol pattern") }
		svc,err:=core.Open(ctx,cfg,true); if err!=nil { log.Fatal(err) }; defer svc.Close(); if svc.Semantic==nil { log.Fatalf("semantic provider unavailable: %s",svc.SemanticError) }
		result,err:=svc.Semantic.FindSymbol(ctx,domain.SymbolQuery{Pattern:flag.Arg(1),Depth:1}); if err!=nil { log.Fatal(err) }; printJSON(result)
	case "mcp":
		svc,err:=core.Open(ctx,cfg,true); if err!=nil { log.Fatal(err) }; defer svc.Close(); server:=mcpserver.New(svc); if err:=server.Run(ctx,&mcp.StdioTransport{}); err!=nil { log.Fatal(err) }
	default: usage(); os.Exit(2)
	}
}
