package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ulaista/usage-ai-on-dev/internal/core"
	"github.com/ulaista/usage-ai-on-dev/internal/developerflow"
)

// NewDeveloper adds only the guarded execution surface that is not part of the
// base server. Project detection, impact and developer-flow planning are
// already registered by New, so their tool definitions remain single-source.
func NewDeveloper(svc *core.Service) *mcp.Server {
	server := New(svc)
	type runArgs struct {
		SessionID string `json:"session_id"`
		Task      string `json:"task"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "brain_developer_run",
		Description: "Run the guarded developer flow. Existing repositories are recovered first; USER_DIRTY is preserved; incomplete discovery, ownership conflicts and hard-risk work escalate before local inference.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args runArgs) (*mcp.CallToolResult, any, error) {
		if args.SessionID == "" || args.Task == "" {
			return nil, nil, fmt.Errorf("session_id and task are required")
		}
		result, err := (developerflow.Engine{Service: svc}).Run(ctx, args.SessionID, args.Task)
		if err != nil {
			return nil, nil, err
		}
		return text(result)
	})
	return server
}
