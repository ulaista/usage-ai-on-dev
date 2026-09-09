package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ulaista/usage-ai-on-dev/internal/core"
)

// NewDeveloper is the developer-oriented entrypoint. New already registers
// the brownfield/project recovery and guarded developer-flow tools, so keeping
// one registration path avoids duplicate MCP tool definitions.
func NewDeveloper(svc *core.Service) *mcp.Server {
	return New(svc)
}
