package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ulaista/usage-ai-on-dev/internal/config"
	contextpkg "github.com/ulaista/usage-ai-on-dev/internal/context"
	"github.com/ulaista/usage-ai-on-dev/internal/core"
	"github.com/ulaista/usage-ai-on-dev/internal/domain"
	"github.com/ulaista/usage-ai-on-dev/internal/mcpserver"
	"github.com/ulaista/usage-ai-on-dev/internal/repomap"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: brain-core [--root PATH] <init|status|context|repo-map|mcp|semantic-find|telemetry>")
}

func printJSON(v any) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(data))
}

func main() {
	root := flag.String("root", ".", "project root")
	flag.Parse()
	if flag.NArg() < 1 {
		usage()
		os.Exit(2)
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := config.Load(abs)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	switch flag.Arg(0) {
	case "init":
		if err := cfg.Save(); err != nil {
			log.Fatal(err)
		}
		svc, err := core.Open(ctx, cfg, false)
		if err != nil {
			log.Fatal(err)
		}
		defer svc.Close()
		printJSON(map[string]any{"state_dir": cfg.StateDir, "database": cfg.DatabasePath, "semantic_provider": cfg.SemanticProvider})

	case "status":
		svc, err := core.Open(ctx, cfg, false)
		if err != nil {
			log.Fatal(err)
		}
		defer svc.Close()
		status, err := svc.Status(ctx)
		if err != nil {
			log.Fatal(err)
		}
		printJSON(status)

	case "context":
		if flag.NArg() < 2 {
			log.Fatal("context requires a task")
		}
		svc, err := core.Open(ctx, cfg, true)
		if err != nil {
			log.Fatal(err)
		}
		defer svc.Close()
		packet, err := (contextpkg.Compiler{Service: svc}).Compile(ctx, flag.Arg(1), cfg.TargetContext)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Print(packet.RenderMarkdown())

	case "repo-map":
		if flag.NArg() < 2 {
			log.Fatal("repo-map requires a task")
		}
		result, err := (repomap.Builder{
			Root:      cfg.Root,
			CachePath: filepath.Join(cfg.StateDir, "cache", "repomap.json"),
			MaxFiles:  cfg.RepoMapMaxFiles,
		}).Build(ctx, repomap.Request{Task: flag.Arg(1), TokenBudget: cfg.RepoMapTokens})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Print(result.RenderMarkdown())

	case "telemetry":
		svc, err := core.Open(ctx, cfg, false)
		if err != nil {
			log.Fatal(err)
		}
		defer svc.Close()
		model := ""
		if flag.NArg() > 1 {
			model = flag.Arg(1)
		}
		stats, err := svc.Store.TelemetrySummary(ctx, model)
		if err != nil {
			log.Fatal(err)
		}
		printJSON(stats)

	case "semantic-find":
		if flag.NArg() < 2 {
			log.Fatal("semantic-find requires a symbol pattern")
		}
		svc, err := core.Open(ctx, cfg, true)
		if err != nil {
			log.Fatal(err)
		}
		defer svc.Close()
		if svc.Semantic == nil {
			log.Fatalf("semantic provider unavailable: %s", svc.SemanticError)
		}
		result, err := svc.Semantic.FindSymbol(ctx, domain.SymbolQuery{Pattern: flag.Arg(1), Depth: 1})
		if err != nil {
			log.Fatal(err)
		}
		printJSON(result)

	case "mcp":
		svc, err := core.Open(ctx, cfg, true)
		if err != nil {
			log.Fatal(err)
		}
		defer svc.Close()
		server := mcpserver.New(svc)
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
			log.Fatal(err)
		}

	default:
		usage()
		os.Exit(2)
	}
}
