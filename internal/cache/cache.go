// Package cache persists small key-value payloads (cost lookups,
// advisor snapshots) across cloudnav runs so the second `c` press after
// a restart doesn't repeat a multi-second Cost Management call.
//
// Two backends ship with cloudnav and share the same Store[T] facade:
//
//   - JSON-per-key files (default) — boring, dependency-free, ideal
//     while the cache stays in the kilobytes range.
//   - SQLite single-file DB — opt-in via CLOUDNAV_CACHE_BACKEND=sqlite,
//     adds indexed lookups and atomic multi-row writes for upcoming
//     cost-history / cross-sub-aggregation features.
//
// The interface stays narrow on purpose: Get / Set / Delete / Clear.
// If an entry's payload cannot be marshalled or read, the call returns
// (zero, false) — callers re-fetch from the cloud.
package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Entry wraps a cached payload with its fetch timestamp for TTL checks.
type Entry[T any] struct {
	FetchedAt time.Time `json:"fetched_at"`
	Payload   T         `json:"payload"`
}

// Backend is the storage primitive Store[T] uses. Implementations move
// raw bytes between the runtime and persistent storage; the typed
// JSON marshalling lives in Store[T] so every backend speaks the same
// on-the-wire shape.
type Backend interface {
	// Read returns the raw bytes + fetch time for (bucket, key). Returns
	// (nil, zero, false) on any miss.
	Read(bucket, key string) ([]byte, time.Time, bool)
	// Write atomically persists data for (bucket, key) at fetchedAt.
	Write(bucket, key string, data []byte, fetchedAt time.Time) error
	// Remove deletes one entry. Silent on missing.
	Remove(bucket, key string) error
	// RemoveBucket drops every entry in the bucket.
	RemoveBucket(bucket string) error
	// Close releases backend resources (file handles / DB connection).
	Close() error
}

// Store is a generic typed cache. Use one Store per "bucket"
// (e.g. costs, advisor, health) so the TTL can be tuned per domain
// and entries can be invalidated in groups.
type Store[T any] struct {
	backend Backend
	bucket  string
	ttl     time.Duration
}

// NewStore returns a store backed by the JSON-per-key file backend
// rooted at baseDir. Kept for compatibility with existing callers.
// Use NewStoreWithBackend when you need to pick the SQLite backend.
func NewStore[T any](baseDir, bucket string, ttl time.Duration) *Store[T] {
	return NewStoreWithBackend[T](NewJSONBackend(baseDir), bucket, ttl)
}

// NewStoreWithBackend returns a Store wired to the given backend.
func NewStoreWithBackend[T any](backend Backend, bucket string, ttl time.Duration) *Store[T] {
	return &Store[T]{backend: backend, bucket: bucket, ttl: ttl}
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

// BackendFromEnv picks a backend based on the CLOUDNAV_CACHE_BACKEND
// env var. Default is the JSON file backend (no behaviour change for
// existing users). "sqlite" / "sql" / "db" select the SQLite backend
// rooted at <Path()>/cloudnav.db.
func BackendFromEnv() Backend {
	choice := strings.ToLower(strings.TrimSpace(os.Getenv("CLOUDNAV_CACHE_BACKEND")))
	switch choice {
	case "sqlite", "sql", "db":
		b, err := NewSQLiteBackend(filepath.Join(Path(), "cloudnav.db"))
		if err != nil {
			// SQLite open failed (read-only fs, locked, missing dir).
			// Falling back to JSON keeps cloudnav usable instead of
			// erroring out at startup.
			fmt.Fprintf(os.Stderr, "cloudnav: sqlite cache unavailable (%v) — using JSON cache\n", err)
			return NewJSONBackend(Path())
		}
		return b
	default:
		return NewJSONBackend(Path())
	}
}

// Get reads a fresh entry for key. Returns (zero, false) on any
// miss / stale / malformed condition — callers treat those the same:
// re-fetch.
func (s *Store[T]) Get(key string) (T, bool) {
	var zero T
	if s == nil {
		return zero, false
	}
	data, fetchedAt, ok := s.backend.Read(s.bucket, key)
	if !ok {
		return zero, false
	}
	if s.ttl > 0 && time.Since(fetchedAt) > s.ttl {
		return zero, false
	}
	var payload T
	if err := json.Unmarshal(data, &payload); err != nil {
		return zero, false
	}
	return payload, true
}

// Set writes the entry. Errors are returned but callers generally
// discard them: a cache write is strictly best-effort, the live fetch
// still succeeded.
func (s *Store[T]) Set(key string, payload T) error {
	if s == nil {
		return nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return s.backend.Write(s.bucket, key, data, time.Now())
}

// Delete removes a single key. Silent on missing (that's a no-op).
func (s *Store[T]) Delete(key string) error {
	if s == nil {
		return nil
	}
	return s.backend.Remove(s.bucket, key)
}

// Clear drops the whole bucket. Used when the user explicitly wants to
// re-fetch (e.g. 'X clear cache' in the TUI).
func (s *Store[T]) Clear() error {
	if s == nil {
		return nil
	}
	return s.backend.RemoveBucket(s.bucket)
}
