package projectmode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Ensure returns the original baseline for an existing developer-flow session.
// It deliberately does not recapture dirty files on repeated calls, because doing
// so would incorrectly reclassify changes made after task start as USER_DIRTY.
func (e Engine) Ensure(ctx context.Context, sessionID, task string) (Baseline, error) {
	if strings.TrimSpace(sessionID) == "" {
		return Baseline{}, fmt.Errorf("session_id is required")
	}
	baseline, err := e.loadBaseline(sessionID)
	if err == nil {
		if baseline.Task != task {
			return Baseline{}, fmt.Errorf("brownfield session %s is already bound to a different task; start a new session to preserve ownership boundaries", sessionID)
		}
		return baseline, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Baseline{}, err
	}
	return e.Begin(ctx, sessionID, task)
}
