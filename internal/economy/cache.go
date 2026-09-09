package economy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

func Fingerprint(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func NormalizeTask(task string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(task)), " ")
}

type FileCache struct {
	Dir string
}

func (c FileCache) path(namespace, key string) string {
	return filepath.Join(c.Dir, namespace, key+".json")
}

func (c FileCache) Load(namespace, key string, out any) (bool, error) {
	data, err := os.ReadFile(c.path(namespace, key))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return false, err
	}
	return true, nil
}

func (c FileCache) Save(namespace, key string, value any) error {
	path := c.path(namespace, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

type flight struct {
	done chan struct{}
	val  any
	err  error
}

type Group struct {
	mu sync.Mutex
	m  map[string]*flight
}

func (g *Group) Do(key string, fn func() (any, error)) (any, error, bool) {
	g.mu.Lock()
	if g.m == nil {
		g.m = map[string]*flight{}
	}
	if existing, ok := g.m[key]; ok {
		g.mu.Unlock()
		<-existing.done
		return existing.val, existing.err, true
	}
	current := &flight{done: make(chan struct{})}
	g.m[key] = current
	g.mu.Unlock()

	current.val, current.err = fn()
	close(current.done)
	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()
	return current.val, current.err, false
}
