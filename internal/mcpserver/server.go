package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	contextpkg "github.com/ulaista/usage-ai-on-dev/internal/context"
	"github.com/ulaista/usage-ai-on-dev/internal/core"
	"github.com/ulaista/usage-ai-on-dev/internal/domain"
	"github.com/ulaista/usage-ai-on-dev/internal/hardware"
	"github.com/ulaista/usage-ai-on-dev/internal/localworker"
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
	server := mcp.NewServer(&mcp.Implementation{Name: "project-brain", Version: "v0.3.0"}, nil)

	type emptyArgs struct{}
	mcp.AddTool(server, &mcp.Tool{Name: "brain_status", Description: "Return Project Brain status, active intent count, storage and telemetry summary."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyArgs) (*mcp.CallToolResult, any, error) {
			status, err := svc.Status(ctx)
			if err != nil {
				return nil, nil, err
			}
			return text(status)
		})

	mcp.AddTool(server, &mcp.Tool{Name: "brain_hardware", Description: "Detect current hardware profile, available memory, swap usage and safe local-model context limits."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyArgs) (*mcp.CallToolResult, any, error) {
			snapshot, err := (hardware.SystemDetector{}).Snapshot(ctx)
			if err != nil {
				return nil, nil, err
			}
			return text(snapshot)
		})

	type contextArgs struct {
		Task      string `json:"task"`
		MaxTokens int    `json:"max_tokens,omitempty"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "brain_context", Description: "Compile task-specific context from Git state, active intents, ranked repository graph and Serena semantic evidence under a token budget."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args contextArgs) (*mcp.CallToolResult, any, error) {
			if args.Task == "" {
				return nil, nil, fmt.Errorf("task is required")
			}
			packet, err := (contextpkg.Compiler{Service: svc}).Compile(ctx, args.Task, args.MaxTokens)
			if err != nil {
				return nil, nil, err
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: packet.RenderMarkdown()}}}, packet, nil
		})

	type workerArgs struct {
		Task          string `json:"task"`
		TaskType      string `json:"task_type,omitempty"`
		Context       string `json:"context,omitempty"`
		ContextTokens int    `json:"context_tokens,omitempty"`
		Compile       bool   `json:"compile_context,omitempty"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "brain_local_run", Description: "Run a bounded task on the local Ollama worker only when current hardware resources are safe. Returns typed evidence or a fallback reason for the strong model."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args workerArgs) (*mcp.CallToolResult, any, error) {
			if args.Task == "" {
				return nil, nil, fmt.Errorf("task is required")
			}
			if args.Compile || args.Context == "" {
				snapshot, err := (hardware.SystemDetector{}).Snapshot(ctx)
				if err != nil {
					return nil, nil, err
				}
				packet, err := (contextpkg.Compiler{Service: svc}).Compile(ctx, args.Task, snapshot.Profile.SoftContextTokens)
				if err != nil {
					return nil, nil, err
				}
				args.Context = packet.RenderMarkdown()
				args.ContextTokens = packet.EstimatedTokens
			}
			worker := localworker.Worker{Model: svc.Config.LocalModel, OllamaURL: svc.Config.OllamaURL, Store: svc.Store}
			result, err := worker.Run(ctx, localworker.Request{Task: args.Task, TaskType: args.TaskType, Context: args.Context, ContextTokens: args.ContextTokens})
			if err != nil {
				return nil, nil, err
			}
			return text(result)
		})

	type repoMapArgs struct {
		Task      string   `json:"task"`
		MaxTokens int      `json:"max_tokens,omitempty"`
		Changed   []string `json:"changed_files,omitempty"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "brain_repo_map", Description: "Build a compact task-ranked repository map from dependency edges, Git changes, semantic seeds and code signatures."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args repoMapArgs) (*mcp.CallToolResult, any, error) {
			if args.Task == "" {
				return nil, nil, fmt.Errorf("task is required")
			}
			budget := args.MaxTokens
			if budget <= 0 {
				budget = svc.Config.RepoMapTokens
			}
			result, err := (repomap.Builder{Root: svc.Config.Root, CachePath: filepath.Join(svc.Config.StateDir, "cache", "repomap.json"), MaxFiles: svc.Config.RepoMapMaxFiles}).Build(ctx, repomap.Request{Task: args.Task, ChangedFiles: args.Changed, TokenBudget: budget})
			if err != nil {
				return nil, nil, err
			}
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
			if args.Title == "" {
				return nil, nil, fmt.Errorf("title is required")
			}
			in, err := svc.CreateIntent(ctx, args.Title, args.Goal, args.Constraints, args.Affected, args.Verification)
			if err != nil {
				return nil, nil, err
			}
			return text(in)
		})

	type batchArgs struct {
		IntentID string   `json:"intent_id"`
		Title    string   `json:"title"`
		Scope    []string `json:"scope,omitempty"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "brain_batch_create", Description: "Create a semantic change batch for an active intent."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args batchArgs) (*mcp.CallToolResult, any, error) {
			if args.IntentID == "" || args.Title == "" {
				return nil, nil, fmt.Errorf("intent_id and title are required")
			}
			b, err := svc.CreateBatch(ctx, args.IntentID, args.Title, args.Scope)
			if err != nil {
				return nil, nil, err
			}
			return text(b)
		})

	mcp.AddTool(server, &mcp.Tool{Name: "brain_intent_list", Description: "List active durable development intents."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyArgs) (*mcp.CallToolResult, any, error) {
			rows, err := svc.Store.ListActiveIntents(ctx)
			if err != nil {
				return nil, nil, err
			}
			return text(rows)
		})

	type telemetryArgs struct{ Model string `json:"model,omitempty"` }
	mcp.AddTool(server, &mcp.Tool{Name: "brain_telemetry", Description: "Summarize measured model latency, tokens, fallbacks and strong-model acceptance."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args telemetryArgs) (*mcp.CallToolResult, any, error) {
			stats, err := svc.Store.TelemetrySummary(ctx, args.Model)
			if err != nil {
				return nil, nil, err
			}
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
			if svc.Semantic == nil {
				return nil, nil, fmt.Errorf("semantic provider is not connected: %s", svc.SemanticError)
			}
			result, err := svc.Semantic.FindSymbol(ctx, domain.SymbolQuery{Pattern: args.Pattern, RelativePath: args.RelativePath, IncludeBody: args.IncludeBody, Depth: args.Depth})
			if err != nil {
				return nil, nil, err
			}
			return text(result)
		})

	type refsArgs struct {
		NamePath     string `json:"name_path"`
		RelativePath string `json:"relative_path"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "brain_find_references", Description: "Find references to a symbol using Serena/LSP instead of scanning the whole repository."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args refsArgs) (*mcp.CallToolResult, any, error) {
			if svc.Semantic == nil {
				return nil, nil, fmt.Errorf("semantic provider is not connected: %s", svc.SemanticError)
			}
			result, err := svc.Semantic.FindReferences(ctx, args.NamePath, args.RelativePath)
			if err != nil {
				return nil, nil, err
			}
			return text(result)
		})

	return server
}
