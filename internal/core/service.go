package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/ulaista/usage-ai-on-dev/internal/config"
	"github.com/ulaista/usage-ai-on-dev/internal/domain"
	"github.com/ulaista/usage-ai-on-dev/internal/semantic"
	"github.com/ulaista/usage-ai-on-dev/internal/store"
)

type Service struct {
	Config        config.Config
	Store         *store.Store
	Semantic      semantic.Provider
	SemanticError string
}

func Open(ctx context.Context, cfg config.Config, connectSemantic bool) (*Service, error) {
	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	svc := &Service{Config: cfg, Store: st}
	if connectSemantic && cfg.SemanticProvider == "serena" {
		provider, err := semantic.ConnectSerena(ctx, cfg)
		if err != nil {
			svc.SemanticError = err.Error()
			return svc, nil
		}
		if err := provider.Activate(ctx, cfg.Root); err != nil {
			_ = provider.Close()
			svc.SemanticError = err.Error()
			return svc, nil
		}
		svc.Semantic = provider
	}
	return svc, nil
}

func (s *Service) Close() error {
	if s.Semantic != nil {
		_ = s.Semantic.Close()
	}
	if s.Store != nil {
		return s.Store.Close()
	}
	return nil
}

func id(prefix string) string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

func (s *Service) CreateIntent(ctx context.Context, title, goal string, constraints, affected, verification []string) (domain.Intent, error) {
	now := time.Now().UTC()
	if goal == "" {
		goal = title
	}
	in := domain.Intent{
		ID: id("INT"), Title: title, Goal: goal, Status: "active",
		Constraints: constraints, Affected: affected, Verification: verification,
		CreatedAt: now, UpdatedAt: now,
	}
	return in, s.Store.CreateIntent(ctx, in)
}

func (s *Service) CreateBatch(ctx context.Context, intentID, title string, scope []string) (domain.Batch, error) {
	now := time.Now().UTC()
	b := domain.Batch{ID: id("BAT"), IntentID: intentID, Title: title, Scope: scope, Status: "active", CreatedAt: now, UpdatedAt: now}
	return b, s.Store.CreateBatch(ctx, b)
}

func (s *Service) Status(ctx context.Context) (map[string]any, error) {
	intents, err := s.Store.ListActiveIntents(ctx)
	if err != nil {
		return nil, err
	}
	telemetry, err := s.Store.TelemetrySummary(ctx, "")
	if err != nil {
		return nil, err
	}
	provider := "disabled"
	if s.Semantic != nil {
		provider = s.Semantic.Name()
	} else if s.Config.SemanticProvider != "" {
		provider = s.Config.SemanticProvider + " (unavailable)"
	}
	status := map[string]any{
		"version":           "0.2.0",
		"root":              s.Config.Root,
		"database":          s.Config.DatabasePath,
		"semantic_provider": provider,
		"active_intents":    len(intents),
		"telemetry":         telemetry,
	}
	if s.SemanticError != "" {
		status["semantic_error"] = s.SemanticError
	}
	return status, nil
}
