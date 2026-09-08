package semantic

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ulaista/usage-ai-on-dev/internal/domain"
	"github.com/ulaista/usage-ai-on-dev/internal/economy"
	"github.com/ulaista/usage-ai-on-dev/internal/gitctx"
)

const semanticCacheVersion = "semantic-cache-v1"

type SavingRecorder interface {
	RecordSaving(context.Context, domain.CacheSaving) error
}

type CachedProvider struct {
	Base     Provider
	Root     string
	StateDir string
	Recorder SavingRecorder
	flights  economy.Group
}

func NewCachedProvider(base Provider, root, stateDir string, recorder SavingRecorder) *CachedProvider {
	return &CachedProvider{Base: base, Root: root, StateDir: stateDir, Recorder: recorder}
}

func (p *CachedProvider) Name() string { return p.Base.Name() }
func (p *CachedProvider) Activate(ctx context.Context, project string) error {
	return p.Base.Activate(ctx, project)
}
func (p *CachedProvider) Close() error { return p.Base.Close() }

func (p *CachedProvider) SymbolsOverview(ctx context.Context, relativePath string, depth int) (domain.SemanticResult, error) {
	args, _ := json.Marshal(struct {
		RelativePath string `json:"relative_path"`
		Depth        int    `json:"depth"`
	}{relativePath, depth})
	return p.cached(ctx, "symbols_overview", string(args), func() (domain.SemanticResult, error) {
		return p.Base.SymbolsOverview(ctx, relativePath, depth)
	})
}

func (p *CachedProvider) FindSymbol(ctx context.Context, query domain.SymbolQuery) (domain.SemanticResult, error) {
	args, _ := json.Marshal(query)
	return p.cached(ctx, "find_symbol", string(args), func() (domain.SemanticResult, error) {
		return p.Base.FindSymbol(ctx, query)
	})
}

func (p *CachedProvider) FindReferences(ctx context.Context, namePath, relativePath string) (domain.SemanticResult, error) {
	args, _ := json.Marshal(struct {
		NamePath     string `json:"name_path"`
		RelativePath string `json:"relative_path"`
	}{namePath, relativePath})
	return p.cached(ctx, "find_references", string(args), func() (domain.SemanticResult, error) {
		return p.Base.FindReferences(ctx, namePath, relativePath)
	})
}

func (p *CachedProvider) cached(ctx context.Context, operation, args string, call func() (domain.SemanticResult, error)) (domain.SemanticResult, error) {
	stateID, err := (gitctx.Provider{Root: p.Root}).StateID(ctx)
	if err != nil {
		return domain.SemanticResult{}, err
	}
	key := economy.Fingerprint(semanticCacheVersion, p.Base.Name(), stateID, operation, args)
	cache := economy.FileCache{Dir: filepath.Join(p.StateDir, "cache")}
	var result domain.SemanticResult
	if hit, loadErr := cache.Load("semantic", key, &result); loadErr == nil && hit {
		result.CacheHit = true
		result.CacheKey = key
		result.RepoStateID = stateID
		p.recordHit(ctx, key, result)
		return result, nil
	}

	value, runErr, joined := p.flights.Do(key, func() (any, error) {
		var second domain.SemanticResult
		if hit, loadErr := cache.Load("semantic", key, &second); loadErr == nil && hit {
			return second, nil
		}
		computed, err := call()
		if err != nil {
			return nil, err
		}
		computed.CacheHit = false
		computed.CacheKey = key
		computed.RepoStateID = stateID
		_ = cache.Save("semantic", key, computed)
		return computed, nil
	})
	if runErr != nil {
		return domain.SemanticResult{}, runErr
	}
	result = value.(domain.SemanticResult)
	result.CacheKey = key
	result.RepoStateID = stateID
	if joined {
		result.CacheHit = true
		p.recordHit(ctx, key, result)
	}
	return result, nil
}

func (p *CachedProvider) recordHit(ctx context.Context, key string, result domain.SemanticResult) {
	if p.Recorder == nil {
		return
	}
	estimatedTokens := len(result.Raw) / 4
	if estimatedTokens < 0 {
		estimatedTokens = 0
	}
	_ = p.Recorder.RecordSaving(ctx, domain.CacheSaving{
		Kind:             "semantic",
		Key:              key,
		SavedInputTokens: estimatedTokens,
		CreatedAt:        time.Now().UTC(),
	})
}

func semanticCacheDebugKey(operation string, depth int) string {
	return operation + ":" + strconv.Itoa(depth)
}
