package semantic

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ulaista/usage-ai-on-dev/internal/config"
	"github.com/ulaista/usage-ai-on-dev/internal/domain"
)

type Serena struct {
	session *mcp.ClientSession
}

func ConnectSerena(ctx context.Context, cfg config.Config) (*Serena, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "project-brain", Version: "v0.2.0"}, nil)
	cmd := exec.Command(cfg.SerenaCommand, cfg.SerenaArgs...)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect serena: %w", err)
	}
	return &Serena{session: session}, nil
}

func (s *Serena) Name() string { return "serena" }

func (s *Serena) Close() error {
	if s.session == nil {
		return nil
	}
	return s.session.Close()
}

func (s *Serena) call(ctx context.Context, name string, args map[string]any) (string, error) {
	res, err := s.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", err
	}
	var parts []string
	for _, item := range res.Content {
		if text, ok := item.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("serena tool %s returned no text content", name)
	}
	return strings.Join(parts, "\n"), nil
}

func (s *Serena) Activate(ctx context.Context, project string) error {
	_, err := s.call(ctx, "activate_project", map[string]any{"project": project})
	return err
}

func (s *Serena) SymbolsOverview(ctx context.Context, relativePath string, depth int) (domain.SemanticResult, error) {
	raw, err := s.call(ctx, "get_symbols_overview", map[string]any{
		"relative_path": relativePath,
		"depth": depth,
	})
	return domain.SemanticResult{Provider: s.Name(), Raw: raw}, err
}

func (s *Serena) FindSymbol(ctx context.Context, query domain.SymbolQuery) (domain.SemanticResult, error) {
	args := map[string]any{
		"name_path_pattern": query.Pattern,
		"relative_path": query.RelativePath,
		"include_body": query.IncludeBody,
		"depth": query.Depth,
	}
	raw, err := s.call(ctx, "find_symbol", args)
	return domain.SemanticResult{Provider: s.Name(), Raw: raw}, err
}

func (s *Serena) FindReferences(ctx context.Context, namePath, relativePath string) (domain.SemanticResult, error) {
	raw, err := s.call(ctx, "find_referencing_symbols", map[string]any{
		"name_path": namePath,
		"relative_path": relativePath,
	})
	return domain.SemanticResult{Provider: s.Name(), Raw: raw}, err
}
