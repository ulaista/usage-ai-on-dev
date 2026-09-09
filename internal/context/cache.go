package contextpkg

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ulaista/usage-ai-on-dev/internal/domain"
	"github.com/ulaista/usage-ai-on-dev/internal/economy"
	"github.com/ulaista/usage-ai-on-dev/internal/gitctx"
)

const contextCacheVersion = "context-v1"

type CachedPacket struct {
	Packet  Packet `json:"packet"`
	StateID string `json:"state_id"`
	Key     string `json:"key"`
	Hit     bool   `json:"cache_hit"`
}

func (c Compiler) CompileCached(ctx context.Context, task string, maxTokens int) (CachedPacket, error) {
	if maxTokens <= 0 {
		maxTokens = c.Service.Config.TargetContext
	}
	stateID, err := (gitctx.Provider{Root: c.Service.Config.Root}).StateID(ctx)
	if err != nil {
		packet, compileErr := c.Compile(ctx, task, maxTokens)
		return CachedPacket{Packet: packet}, compileErr
	}
	intents, err := c.Service.Store.ListActiveIntents(ctx)
	if err != nil {
		return CachedPacket{}, err
	}
	intentJSON, _ := json.Marshal(intents)
	key := economy.Fingerprint(contextCacheVersion, economy.NormalizeTask(task), strconv.Itoa(maxTokens), stateID, string(intentJSON), c.Service.Config.SemanticProvider, strconv.Itoa(c.Service.Config.RepoMapTokens))
	cache := economy.FileCache{Dir: filepath.Join(c.Service.Config.StateDir, "cache")}
	var packet Packet
	if hit, loadErr := cache.Load("context", key, &packet); loadErr == nil && hit {
		_ = c.Service.Store.RecordSaving(ctx, domain.CacheSaving{Kind: "context", Key: key, SavedInputTokens: packet.EstimatedTokens, CreatedAt: time.Now().UTC()})
		return CachedPacket{Packet: packet, StateID: stateID, Key: key, Hit: true}, nil
	}
	packet, err = c.Compile(ctx, task, maxTokens)
	if err != nil {
		return CachedPacket{}, err
	}
	if err := cache.Save("context", key, packet); err != nil {
		packet.Warnings = append(packet.Warnings, fmt.Sprintf("context cache save: %v", err))
	}
	return CachedPacket{Packet: packet, StateID: stateID, Key: key}, nil
}
