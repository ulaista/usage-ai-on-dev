package semantic

import (
	"context"

	"github.com/ulaista/usage-ai-on-dev/internal/domain"
)

type Provider interface {
	Name() string
	Activate(context.Context, string) error
	SymbolsOverview(context.Context, string, int) (domain.SemanticResult, error)
	FindSymbol(context.Context, domain.SymbolQuery) (domain.SemanticResult, error)
	FindReferences(context.Context, string, string) (domain.SemanticResult, error)
	Close() error
}
