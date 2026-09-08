package semantic

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ulaista/usage-ai-on-dev/internal/config"
	"github.com/ulaista/usage-ai-on-dev/internal/domain"
)

type LifecycleStatus struct {
	Connected     bool   `json:"connected"`
	ActiveProject string `json:"active_project,omitempty"`
	Reconnects    int    `json:"reconnects"`
}

type Serena struct {
	mu            sync.Mutex
	cfg           config.Config
	session       *mcp.ClientSession
	activeProject string
	reconnects    int
}

func ConnectSerena(ctx context.Context, cfg config.Config) (*Serena, error) {
	s := &Serena{cfg: cfg}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.connectLocked(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Serena) Name() string { return "serena" }

func (s *Serena) Status() LifecycleStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return LifecycleStatus{Connected: s.session != nil, ActiveProject: s.activeProject, Reconnects: s.reconnects}
}

func (s *Serena) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeLocked()
}

func (s *Serena) connectLocked(ctx context.Context) error {
	if s.session != nil {
		return nil
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "project-brain", Version: "v0.8.0"}, nil)
	cmd := exec.CommandContext(ctx, s.cfg.SerenaCommand, s.cfg.SerenaArgs...)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return fmt.Errorf("connect serena: %w", err)
	}
	s.session = session
	return nil
}

func (s *Serena) closeLocked() error {
	if s.session == nil {
		return nil
	}
	err := s.session.Close()
	s.session = nil
	return err
}

func (s *Serena) callOnceLocked(ctx context.Context, name string, args map[string]any) (string, error) {
	if s.session == nil {
		return "", fmt.Errorf("serena session is not connected")
	}
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

func (s *Serena) call(ctx context.Context, name string, args map[string]any) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.connectLocked(ctx); err != nil {
		return "", err
	}
	raw, err := s.callOnceLocked(ctx, name, args)
	if err == nil {
		return raw, nil
	}

	firstErr := err
	_ = s.closeLocked()
	if err := s.connectLocked(ctx); err != nil {
		return "", fmt.Errorf("serena call %s failed (%v) and reconnect failed: %w", name, firstErr, err)
	}
	s.reconnects++
	if s.activeProject != "" && name != "activate_project" {
		if _, err := s.callOnceLocked(ctx, "activate_project", map[string]any{"project": s.activeProject}); err != nil {
			return "", fmt.Errorf("serena reactivation after reconnect failed: %w", err)
		}
	}
	raw, err = s.callOnceLocked(ctx, name, args)
	if err != nil {
		return "", fmt.Errorf("serena call %s failed after reconnect: %w", name, err)
	}
	return raw, nil
}

func (s *Serena) Activate(ctx context.Context, project string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != nil && s.activeProject == project {
		return nil
	}
	if err := s.connectLocked(ctx); err != nil {
		return err
	}
	if _, err := s.callOnceLocked(ctx, "activate_project", map[string]any{"project": project}); err != nil {
		_ = s.closeLocked()
		if err2 := s.connectLocked(ctx); err2 != nil {
			return fmt.Errorf("activate serena project failed (%v), reconnect failed: %w", err, err2)
		}
		s.reconnects++
		if _, err2 := s.callOnceLocked(ctx, "activate_project", map[string]any{"project": project}); err2 != nil {
			return fmt.Errorf("activate serena project failed after reconnect: %w", err2)
		}
	}
	s.activeProject = project
	return nil
}

func (s *Serena) SymbolsOverview(ctx context.Context, relativePath string, depth int) (domain.SemanticResult, error) {
	raw, err := s.call(ctx, "get_symbols_overview", map[string]any{
		"relative_path": relativePath,
		"depth":         depth,
	})
	return domain.SemanticResult{Provider: s.Name(), Raw: raw}, err
}

func (s *Serena) FindSymbol(ctx context.Context, query domain.SymbolQuery) (domain.SemanticResult, error) {
	args := map[string]any{
		"name_path_pattern": query.Pattern,
		"relative_path":     query.RelativePath,
		"include_body":      query.IncludeBody,
		"depth":             query.Depth,
	}
	raw, err := s.call(ctx, "find_symbol", args)
	return domain.SemanticResult{Provider: s.Name(), Raw: raw}, err
}

func (s *Serena) FindReferences(ctx context.Context, namePath, relativePath string) (domain.SemanticResult, error) {
	raw, err := s.call(ctx, "find_referencing_symbols", map[string]any{
		"name_path":     namePath,
		"relative_path": relativePath,
	})
	return domain.SemanticResult{Provider: s.Name(), Raw: raw}, err
}
