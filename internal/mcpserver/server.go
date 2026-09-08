package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	contextpkg "github.com/ulaista/usage-ai-on-dev/internal/context"
	"github.com/ulaista/usage-ai-on-dev/internal/core"
	"github.com/ulaista/usage-ai-on-dev/internal/domain"
	"github.com/ulaista/usage-ai-on-dev/internal/repomap"
)

func text(v any) (*mcp.CallToolResult, any, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, v, nil
}

func New(svc *core.Service) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "project-brain", Version: "v0.2.0"}, nil)

	type statusArgs struct{}
	mcp.AddTool(server, &mcp.Tool{Name: "brain_status", Description: "Return Project Brain status, active intent count, storage and telemetry summary."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ statusArgs) (*mcp.CallToolResult, any, error) {
			status, err := svc.Status(ctx)
			if err != nil { return nil, nil, err }
			return text(status)
		})

	type contextArgs struct {
		Task      string `json:"task"`
		MaxTokens int    `json:"max_tokens,omitempty"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "brain_context", Description: "Compile task-specific context from Git state, active intents, ranked repository graph and Serena semantic evidence under a token budget."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args contextArgs) (*mcp.CallToolResult, any, error) {
			if args.Task == "" { return nil, nil, fmt.Errorf("task is required") }
			packet, err := (contextpkg.Compiler{Service: svc}).Compile(ctx, args.Task, args.MaxTokens)
			if err != nil { return nil, nil, err }
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: packet.RenderMarkdown()}}}, packet, nil
		})

	type repoMapArgs struct {
		Task      string   `json:"task"`
		MaxTokens int      `json:"max_tokens,omitempty"`
		Changed   []string `json:"changed_files,omitempty"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "brain_repo_map", Description: "Build a compact task-ranked repository map from dependency edges, changed-file seeds and code signatures."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args repoMapArgs) (*mcp.CallToolResult, any, error) {
			if args.Task == "" { return nil, nil, fmt.Errorf("task is required") }
			budget := args.MaxTokens
			if budget <= 0 { budget = max(1000, svc.Config.TargetContext/5) }
			result, err := (repomap.Builder{Root: svc.Config.Root}).Build(ctx, repomap.Request{Task: args.Task, ChangedFiles: args.Changed, TokenBudget: budget})
			if err != nil { return nil, nil, err }
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: result.RenderMarkdown()}}}, result, nil
		})

	type createIntentArgs struct {
		Title        string   `json:"title" jsonschema:"short intent title"`
		Goal         string   `json:"goal,omitempty" jsonschema:"desired end state"`
		Constraints  []string `json:"constraints,omitempty"`
		Affected     []string `json:"affected,omitempty"`
		Verification []string `json:"verification,omitempty"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "brain_intent_create", Description: "Create durable development intent before multi-step work."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args createIntentArgs) (*mcp.CallToolResult, any, error) {
			if args.Title == "" { return nil, nil, fmt.Errorf("title is required") }
			in, err := svc.CreateIntent(ctx, args.Title, args.Goal, args.Constraints, args.Affected, args.Verification)
			if err != nil { return nil, nil, err }
			return text(in)
		})

	type batchArgs struct {
		IntentID string   `json:"intent_id"`
		Title    string   `json:"title"`
		Scope    []string `json:"scope,omitempty"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "brain_batch_create", Description: "Create a semantic change batch for an active intent."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args batchArgs) (*mcp.CallToolResult, any, error) {
			if args.IntentID == "" || args.Title == "" { return nil, nil, fmt.Errorf("intent_id and title are required") }
			b, err := svc.CreateBatch(ctx, args.IntentID, args.Title, args.Scope)
			if err != nil { return nil, nil, err }
			return text(b)
		})

	type listArgs struct{}
	mcp.AddTool(server, &mcp.Tool{Name: "brain_intent_list", Description: "List active durable development intents."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ listArgs) (*mcp.CallToolResult, any, error) {
			rows, err := svc.Store.ListActiveIntents(ctx)
			if err != nil { return nil, nil, err }
			return text(rows)
		})

	type telemetryArgs struct { Model string `json:"model,omitempty"` }
	mcp.AddTool(server, &mcp.Tool{Name: "brain_telemetry", Description: "Summarize measured model latency, tokens, fallbacks and strong-model acceptance."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args telemetryArgs) (*mcp.CallToolResult, any, error) {
			stats, err := svc.Store.TelemetrySummary(ctx, args.Model)
			if err != nil { return nil, nil, err }
			return text(stats)
		})

	type symbolArgs struct {
		Pattern      string `json:"pattern"`
		RelativePath string `json:"relative_path,omitempty"`
		IncludeBody  bool   `json:"include_body,omitempty"`
		Depth        int    `json:"depth,omitempty"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "brain_find_symbol", Description: "Find code symbols through the configured semantic provider (Serena/LSP by default)."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args symbolArgs) (*mcp.CallToolResult, any, error) {
			if svc.Semantic == nil { return nil, nil, fmt.Errorf("semantic provider is not connected: %s", svc.SemanticError) }
			result, err := svc.Semantic.FindSymbol(ctx, domain.SymbolQuery{Pattern: args.Pattern, RelativePath: args.RelativePath, IncludeBody: args.IncludeBody, Depth: args.Depth})
			if err != nil { return nil, nil, err }
			return text(result)
		})

	type refsArgs struct {
		NamePath     string `json:"name_path"`
		RelativePath string `json:"relative_path"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "brain_find_references", Description: "Find references to a symbol using Serena/LSP instead of scanning the whole repository."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args refsArgs) (*mcp.CallToolResult, any, error) {
			if svc.Semantic == nil { return nil, nil, fmt.Errorf("semantic provider is not connected: %s", svc.SemanticError) }
			result, err := svc.Semantic.FindReferences(ctx, args.NamePath, args.RelativePath)
			if err != nil { return nil, nil, err }
			return text(result)
		})

	return server
}
