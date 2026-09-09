package main

import (
	"context"
	"log"

	"github.com/ulaista/usage-ai-on-dev/internal/config"
	"github.com/ulaista/usage-ai-on-dev/internal/core"
	"github.com/ulaista/usage-ai-on-dev/internal/developerflow"
	"github.com/ulaista/usage-ai-on-dev/internal/projectmode"
)

func handleDeveloperCommand(ctx context.Context, cfg config.Config, command string, args []string) bool {
	engine := projectmode.Engine{Root:cfg.Root,StateDir:cfg.StateDir}
	switch command {
	case "project-detect":
		project,err:=engine.Detect(ctx);if err!=nil{log.Fatal(err)};printJSON(project);return true
	case "project-begin":
		if len(args)<2{log.Fatal("project-begin requires session-id and task")};baseline,err:=engine.Begin(ctx,args[0],args[1]);if err!=nil{log.Fatal(err)};printJSON(baseline);return true
	case "project-impact":
		if len(args)<1{log.Fatal("project-impact requires session-id")};impact,err:=engine.Impact(ctx,args[0]);if err!=nil{log.Fatal(err)};printJSON(impact);return true
	case "dev-plan","dev-run":
		if len(args)<2{log.Fatalf("%s requires session-id and task",command)}
		svc,err:=core.Open(ctx,cfg,true);if err!=nil{log.Fatal(err)};defer svc.Close()
		flow:=developerflow.Engine{Service:svc}
		if command=="dev-plan"{plan,err:=flow.Prepare(ctx,args[0],args[1]);if err!=nil{log.Fatal(err)};printJSON(plan)}else{result,err:=flow.Run(ctx,args[0],args[1]);if err!=nil{log.Fatal(err)};printJSON(result)}
		return true
	default:
		return false
	}
}
