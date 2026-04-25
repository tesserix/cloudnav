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

// JSONBackend stores each (bucket, key) as <baseDir>/<bucket>/<key>.json,
// matching cloudnav's pre-SQLite layout. Atomic writes via tmp+rename.
// One mutex serialises rename calls so a concurrent Save doesn't lose
// the tmp file.
type JSONBackend struct {
	baseDir string
	mu      sync.Mutex
}

// NewJSONBackend returns a backend rooted at baseDir.
func NewJSONBackend(baseDir string) *JSONBackend {
	return &JSONBackend{baseDir: baseDir}
}

// jsonEntry is the on-disk wire format. Kept separate from Entry[T]
// so the backend stays type-erased.
type jsonEntry struct {
	FetchedAt time.Time       `json:"fetched_at"`
	Payload   json.RawMessage `json:"payload"`
}

func (b *JSONBackend) Read(bucket, key string) ([]byte, time.Time, bool) {
	data, err := os.ReadFile(b.path(bucket, key))
	if err != nil {
		return nil, time.Time{}, false
	}
	var e jsonEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, time.Time{}, false
	}
	return e.Payload, e.FetchedAt, true
}

func (b *JSONBackend) Write(bucket, key string, payload []byte, fetchedAt time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	dir := filepath.Join(b.baseDir, sanitise(bucket))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(jsonEntry{FetchedAt: fetchedAt, Payload: payload})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
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
	if err := os.Rename(tmpName, b.path(bucket, key)); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func (b *JSONBackend) Remove(bucket, key string) error {
	err := os.Remove(b.path(bucket, key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func (b *JSONBackend) RemoveBucket(bucket string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return os.RemoveAll(filepath.Join(b.baseDir, sanitise(bucket)))
}

func (b *JSONBackend) Close() error { return nil }

func (b *JSONBackend) path(bucket, key string) string {
	return filepath.Join(b.baseDir, sanitise(bucket), sanitise(key)+".json")
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
	if len(out) > 200 {
		out = out[:200]
	}
	return string(out)
}
