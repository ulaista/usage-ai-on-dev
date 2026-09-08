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
	"github.com/ulaista/usage-ai-on-dev/internal/core"
	"github.com/ulaista/usage-ai-on-dev/internal/domain"
	"github.com/ulaista/usage-ai-on-dev/internal/mcpserver"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: brain-core [--root PATH] <init|status|mcp|semantic-find|telemetry>")
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
	if err != nil { log.Fatal(err) }
	cfg, err := config.Load(abs)
	if err != nil { log.Fatal(err) }
	ctx := context.Background()

	switch flag.Arg(0) {
	case "init":
		if err := cfg.Save(); err != nil { log.Fatal(err) }
		svc, err := core.Open(ctx, cfg, false)
		if err != nil { log.Fatal(err) }
		defer svc.Close()
		printJSON(map[string]any{"state_dir": cfg.StateDir, "database": cfg.DatabasePath, "semantic_provider": cfg.SemanticProvider})

	case "status":
		svc, err := core.Open(ctx, cfg, false)
		if err != nil { log.Fatal(err) }
		defer svc.Close()
		status, err := svc.Status(ctx)
		if err != nil { log.Fatal(err) }
		printJSON(status)

	case "telemetry":
		svc, err := core.Open(ctx, cfg, false)
		if err != nil { log.Fatal(err) }
		defer svc.Close()
		model := ""
		if flag.NArg() > 1 { model = flag.Arg(1) }
		stats, err := svc.Store.TelemetrySummary(ctx, model)
		if err != nil { log.Fatal(err) }
		printJSON(stats)

	case "semantic-find":
		if flag.NArg() < 2 { log.Fatal("semantic-find requires a symbol pattern") }
		svc, err := core.Open(ctx, cfg, true)
		if err != nil { log.Fatal(err) }
		defer svc.Close()
		result, err := svc.Semantic.FindSymbol(ctx, domain.SymbolQuery{Pattern: flag.Arg(1), Depth: 1})
		if err != nil { log.Fatal(err) }
		printJSON(result)

	case "mcp":
		svc, err := core.Open(ctx, cfg, true)
		if err != nil { log.Fatal(err) }
		defer svc.Close()
		server := mcpserver.New(svc)
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil { log.Fatal(err) }

	default:
		usage()
		os.Exit(2)
	}
}
