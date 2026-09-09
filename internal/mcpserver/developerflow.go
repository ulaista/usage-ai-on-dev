package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ulaista/usage-ai-on-dev/internal/core"
	"github.com/ulaista/usage-ai-on-dev/internal/developerflow"
	"github.com/ulaista/usage-ai-on-dev/internal/projectmode"
)

func addDeveloperFlowTools(server *mcp.Server, svc *core.Service) {
	type flowArgs struct { SessionID string `json:"session_id"`; Task string `json:"task"` }
	mcp.AddTool(server,&mcp.Tool{Name:"brain_developer_flow",Description:"Prepare a developer task end-to-end. Detect new vs existing project, preserve pre-existing dirty changes, recover brownfield context, analyze impact/regression history, compile context and choose the AI route before modification."},func(ctx context.Context,_ *mcp.CallToolRequest,args flowArgs)(*mcp.CallToolResult,any,error){if args.SessionID==""||args.Task==""{return nil,nil,fmt.Errorf("session_id and task are required")};plan,err:=(developerflow.Engine{Service:svc}).Prepare(ctx,args.SessionID,args.Task);if err!=nil{return nil,nil,err};return text(plan)})
	mcp.AddTool(server,&mcp.Tool{Name:"brain_developer_run",Description:"Run the guarded developer flow. Existing repositories are recovered first; USER_DIRTY is preserved; incomplete discovery or hard-risk work escalates before local inference."},func(ctx context.Context,_ *mcp.CallToolRequest,args flowArgs)(*mcp.CallToolResult,any,error){if args.SessionID==""||args.Task==""{return nil,nil,fmt.Errorf("session_id and task are required")};result,err:=(developerflow.Engine{Service:svc}).Run(ctx,args.SessionID,args.Task);if err!=nil{return nil,nil,err};return text(result)})
	mcp.AddTool(server,&mcp.Tool{Name:"brain_project_detect",Description:"Mechanically detect whether the repository is new or existing and report languages, frameworks, build/test tools, CI and dirty state without an AI call."},func(ctx context.Context,_ *mcp.CallToolRequest,_ struct{})(*mcp.CallToolResult,any,error){project,err:=(projectmode.Engine{Root:svc.Config.Root,StateDir:svc.Config.StateDir}).Detect(ctx);if err!=nil{return nil,nil,err};return text(project)})
	mcp.AddTool(server,&mcp.Tool{Name:"brain_project_begin",Description:"Capture or reuse the stable developer-session baseline, including BASE and pre-existing USER_DIRTY file hashes."},func(ctx context.Context,_ *mcp.CallToolRequest,args flowArgs)(*mcp.CallToolResult,any,error){if args.SessionID==""||args.Task==""{return nil,nil,fmt.Errorf("session_id and task are required")};baseline,err:=(projectmode.Engine{Root:svc.Config.Root,StateDir:svc.Config.StateDir}).Ensure(ctx,args.SessionID,args.Task);if err!=nil{return nil,nil,err};return text(baseline)})
	type impactArgs struct { SessionID string `json:"session_id"` }
	mcp.AddTool(server,&mcp.Tool{Name:"brain_project_impact",Description:"Re-evaluate an existing developer-flow baseline and return affected files, tests/config, regression window and BASE/USER_DIRTY/BRAIN_DELTA ownership."},func(ctx context.Context,_ *mcp.CallToolRequest,args impactArgs)(*mcp.CallToolResult,any,error){if args.SessionID==""{return nil,nil,fmt.Errorf("session_id is required")};impact,err:=(projectmode.Engine{Root:svc.Config.Root,StateDir:svc.Config.StateDir}).Impact(ctx,args.SessionID);if err!=nil{return nil,nil,err};return text(impact)})
}
