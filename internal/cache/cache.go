// Package cache persists small key-value payloads (cost lookups,
// advisor snapshots) across cloudnav runs so the second `c` press after
// a restart doesn't repeat a multi-second Cost Management call.
//
// Implementation is deliberately boring: one JSON file per bucket,
// atomic writes via tmp+rename, opportunistic reads. No external deps
// (no SQLite, no bolt) — cloudnav caches kilobytes, not megabytes, and
// a single-writer process doesn't need row-level locking. The interface
// is narrow enough that swapping to SQLite later stays a ~100-line PR.
package cache

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry wraps a cached payload with its fetch timestamp for TTL checks.
type Entry[T any] struct {
	FetchedAt time.Time `json:"fetched_at"`
	Payload   T         `json:"payload"`
}

// Store is a generic on-disk cache keyed by arbitrary strings. Use one
// Store per "bucket" (e.g. costs, advisor, health) so the TTL can be
// tuned per domain and entries can be invalidated in groups.
type Store[T any] struct {
	dir string
	ttl time.Duration

	mu sync.Mutex // serialises writes so two Save()s don't race the rename
}

// NewStore returns a store rooted at baseDir/bucket. Entries older than
// ttl are treated as misses even if they still exist on disk, so a
// stale cost column doesn't haunt the TUI across days.
func NewStore[T any](baseDir, bucket string, ttl time.Duration) *Store[T] {
	return &Store[T]{
		dir: filepath.Join(baseDir, bucket),
		ttl: ttl,
	}
}

// Path returns the OS-specific cache directory cloudnav uses. Honours
// $CLOUDNAV_CACHE so CI / tests can redirect without touching the real
// ~/.cache tree.
func Path() string {
	if v := os.Getenv("CLOUDNAV_CACHE"); v != "" {
		return v
	}
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "cloudnav")
	}
	return filepath.Join(os.Getenv("HOME"), ".cache", "cloudnav")
}

// Get reads a fresh entry for key. Returns (zero, false) on any
// miss / stale / malformed condition — callers treat those the same:
// re-fetch.
func (s *Store[T]) Get(key string) (T, bool) {
	var zero T
	if s == nil {
		return zero, false
	}
	data, err := os.ReadFile(s.path(key))
	if err != nil {
		return zero, false
	}
	var e Entry[T]
	if err := json.Unmarshal(data, &e); err != nil {
		return zero, false
	}
	if s.ttl > 0 && time.Since(e.FetchedAt) > s.ttl {
		return zero, false
	}
	return e.Payload, true
}

// Set writes the entry atomically. Errors are returned but callers
// generally discard them: a cache write is strictly best-effort, the
// live fetch still succeeded.
func (s *Store[T]) Set(key string, payload T) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(Entry[T]{FetchedAt: time.Now(), Payload: payload})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, s.path(key)); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// Delete removes a single key. Silent on missing (that's a no-op).
func (s *Store[T]) Delete(key string) error {
	if s == nil {
		return nil
	}
	err := os.Remove(s.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Clear drops the whole bucket. Used when the user explicitly wants to
// re-fetch (e.g. 'X clear cache' in the TUI).
func (s *Store[T]) Clear() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.RemoveAll(s.dir)
}

// path returns a safe on-disk filename for key. We hash-like-encode by
// replacing path separators so a key can contain /, :, etc. without
// escaping the bucket directory.
func (s *Store[T]) path(key string) string {
	safe := sanitise(key)
	return filepath.Join(s.dir, safe+".json")
}

func sanitise(s string) string {
	// Swap characters that would break a filename on any of the three
	// target OSs (/, \, :, *, ?, ", <, >, |). Collapse whitespace too so
	// the resulting name stays one token.
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', ' ', '\t':
			out = append(out, '_')
		default:
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return "empty"
	}
	// Defensive length cap — most filesystems stop accepting at 255.
	if len(out) > 200 {
		out = out[:200]
	}
	return string(out)
}
