package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ulaista/usage-ai-on-dev/internal/core"
	"github.com/ulaista/usage-ai-on-dev/internal/developerflow"
	"github.com/ulaista/usage-ai-on-dev/internal/projectmode"
)

func NewDeveloper(svc *core.Service) *mcp.Server {
	server := New(svc)
	addBrownfieldTools(server, svc)
	return server
}

func addBrownfieldTools(server *mcp.Server, svc *core.Service) {
	type emptyArgs struct{}
	type sessionArgs struct { SessionID string `json:"session_id"`; Task string `json:"task"` }
	mcp.AddTool(server,&mcp.Tool{Name:"brain_project_detect",Description:"Detect whether the repository is new or existing and report languages, build/test/CI tooling and current dirty state."},func(ctx context.Context,_ *mcp.CallToolRequest,_ emptyArgs)(*mcp.CallToolResult,any,error){p,err:=(projectmode.Engine{Root:svc.Config.Root,StateDir:svc.Config.StateDir}).Detect(ctx);if err!=nil{return nil,nil,err};return text(p)})
	mcp.AddTool(server,&mcp.Tool{Name:"brain_project_begin",Description:"Capture an existing-project baseline before AI work, including pre-existing USER_DIRTY changes that must be preserved."},func(ctx context.Context,_ *mcp.CallToolRequest,args sessionArgs)(*mcp.CallToolResult,any,error){if args.SessionID==""||args.Task==""{return nil,nil,fmt.Errorf("session_id and task are required")};b,err:=(projectmode.Engine{Root:svc.Config.Root,StateDir:svc.Config.StateDir}).Begin(ctx,args.SessionID,args.Task);if err!=nil{return nil,nil,err};return text(b)})
	mcp.AddTool(server,&mcp.Tool{Name:"brain_project_impact",Description:"Recompute brownfield impact, ownership, related tests/config, history and regression window against the captured baseline."},func(ctx context.Context,_ *mcp.CallToolRequest,args sessionArgs)(*mcp.CallToolResult,any,error){if args.SessionID==""{return nil,nil,fmt.Errorf("session_id is required")};impact,err:=(projectmode.Engine{Root:svc.Config.Root,StateDir:svc.Config.StateDir}).Impact(ctx,args.SessionID);if err!=nil{return nil,nil,err};return text(impact)})
	mcp.AddTool(server,&mcp.Tool{Name:"brain_dev_plan",Description:"Prepare the full developer-request flow: project recovery, ownership, semantic/context discovery, route decision, mechanical-step count and planned AI calls."},func(ctx context.Context,_ *mcp.CallToolRequest,args sessionArgs)(*mcp.CallToolResult,any,error){if args.SessionID==""||args.Task==""{return nil,nil,fmt.Errorf("session_id and task are required")};plan,err:=(developerflow.Engine{Service:svc}).Prepare(ctx,args.SessionID,args.Task);if err!=nil{return nil,nil,err};return text(plan)})
	mcp.AddTool(server,&mcp.Tool{Name:"brain_dev_run",Description:"Run the guarded developer flow. Existing projects are recovered first; high-risk or incomplete discovery routes to strong ownership without local inference."},func(ctx context.Context,_ *mcp.CallToolRequest,args sessionArgs)(*mcp.CallToolResult,any,error){if args.SessionID==""||args.Task==""{return nil,nil,fmt.Errorf("session_id and task are required")};result,err:=(developerflow.Engine{Service:svc}).Run(ctx,args.SessionID,args.Task);if err!=nil{return nil,nil,err};return text(result)})
}
