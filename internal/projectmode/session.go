package projectmode

import (
	"context"
	"errors"
	"os"
)

// Ensure preserves the first baseline for the same developer task/session.
// Later iterations must not reclassify BRAIN_DELTA as pre-existing USER_DIRTY.
// Reusing a session id for a different task intentionally starts a new baseline.
func (e Engine) Ensure(ctx context.Context, sessionID, task string) (Baseline, error) {
	existing, err := e.loadBaseline(sessionID)
	if err == nil && existing.Version == baselineVersion && existing.Task == task {
		return existing, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Baseline{}, err
	}
	return e.Begin(ctx, sessionID, task)
}
