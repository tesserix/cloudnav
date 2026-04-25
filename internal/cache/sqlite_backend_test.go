package cache

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// newSQLiteStore opens a fresh SQLite-backed Store[T] in a temp dir.
// The temp dir is removed by t.Cleanup; we explicitly Close() the
// backend so WAL files are flushed.
func newSQLiteStore[T any](t *testing.T, bucket string, ttl time.Duration) (*Store[T], *SQLiteBackend) {
	t.Helper()
	dir := t.TempDir()
	b, err := NewSQLiteBackend(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open sqlite backend: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return NewStoreWithBackend[T](b, bucket, ttl), b
}

func TestSQLiteRoundTrip(t *testing.T) {
	s, _ := newSQLiteStore[map[string]string](t, "costs", time.Minute)
	want := map[string]string{"rg1": "£12", "rg2": "£99"}
	if err := s.Set("sub-A", want); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok := s.Get("sub-A")
	if !ok {
		t.Fatal("Get miss after Set")
	}
	if got["rg1"] != "£12" || got["rg2"] != "£99" {
		t.Errorf("payload mismatch: %v", got)
	}
}

func TestSQLiteUpsertOverwrites(t *testing.T) {
	s, _ := newSQLiteStore[int](t, "n", time.Minute)
	_ = s.Set("k", 1)
	_ = s.Set("k", 2)
	got, ok := s.Get("k")
	if !ok || got != 2 {
		t.Errorf("upsert: got (%v, %v), want (2, true)", got, ok)
	}
}

func TestSQLiteTTLExpiry(t *testing.T) {
	s, _ := newSQLiteStore[int](t, "n", time.Millisecond)
	_ = s.Set("k", 42)
	time.Sleep(5 * time.Millisecond)
	if _, ok := s.Get("k"); ok {
		t.Error("Get should treat past-TTL row as miss")
	}
}

func TestSQLiteDelete(t *testing.T) {
	s, _ := newSQLiteStore[int](t, "n", time.Minute)
	_ = s.Set("k", 1)
	if err := s.Delete("k"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("k"); ok {
		t.Error("Get should miss after Delete")
	}
	if err := s.Delete("k"); err != nil {
		t.Errorf("Delete on missing key should be no-op, got %v", err)
	}
}

func TestSQLiteClearOnlyTouchesOwnBucket(t *testing.T) {
	dir := t.TempDir()
	b, err := NewSQLiteBackend(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })

	costs := NewStoreWithBackend[int](b, "costs", time.Minute)
	pim := NewStoreWithBackend[int](b, "pim", time.Minute)

	_ = costs.Set("a", 1)
	_ = costs.Set("b", 2)
	_ = pim.Set("a", 99)

	if err := costs.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, ok := costs.Get("a"); ok {
		t.Error("Clear should drop costs/a")
	}
	if got, ok := pim.Get("a"); !ok || got != 99 {
		t.Errorf("Clear on costs bucket leaked into pim: got (%v, %v)", got, ok)
	}
}

func TestSQLiteConcurrentWrites(t *testing.T) {
	// Single-connection pool + WAL means concurrent writers serialise
	// cleanly without "database is locked" errors. This guards the
	// pool-size guarantee in NewSQLiteBackend.
	s, _ := newSQLiteStore[int](t, "n", time.Minute)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			if err := s.Set("k", v); err != nil {
				t.Errorf("Set: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if _, ok := s.Get("k"); !ok {
		t.Error("expected key to exist after concurrent writes")
	}
}

// TestSQLitePersistsAcrossReopens guarantees data survives Close + reopen
// — the property JSON-per-file already had implicitly via the filesystem.
func TestSQLitePersistsAcrossReopens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	b1, err := NewSQLiteBackend(path)
	if err != nil {
		t.Fatal(err)
	}
	s1 := NewStoreWithBackend[string](b1, "n", time.Hour)
	_ = s1.Set("hello", "world")
	_ = b1.Close()

	b2, err := NewSQLiteBackend(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b2.Close() }()
	s2 := NewStoreWithBackend[string](b2, "n", time.Hour)
	got, ok := s2.Get("hello")
	if !ok || got != "world" {
		t.Errorf("after reopen: got (%q, %v), want (\"world\", true)", got, ok)
	}
}

// TestBackendParity asserts the JSON and SQLite backends behave
// identically for the Get/Set/Delete/Clear API the rest of cloudnav
// relies on. New backends must pass this matrix.
func TestBackendParity(t *testing.T) {
	cases := []struct {
		name string
		make func(t *testing.T) Backend
	}{
		{
			name: "json",
			make: func(t *testing.T) Backend { return NewJSONBackend(t.TempDir()) },
		},
		{
			name: "sqlite",
			make: func(t *testing.T) Backend {
				b, err := NewSQLiteBackend(filepath.Join(t.TempDir(), "p.db"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = b.Close() })
				return b
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := c.make(t)
			s := NewStoreWithBackend[string](b, "bucket", time.Minute)

			if _, ok := s.Get("x"); ok {
				t.Errorf("%s: empty cache hit", c.name)
			}
			if err := s.Set("x", "v1"); err != nil {
				t.Fatalf("%s: Set: %v", c.name, err)
			}
			if got, ok := s.Get("x"); !ok || got != "v1" {
				t.Errorf("%s: round-trip got (%q, %v)", c.name, got, ok)
			}
			if err := s.Delete("x"); err != nil {
				t.Fatalf("%s: Delete: %v", c.name, err)
			}
			if _, ok := s.Get("x"); ok {
				t.Errorf("%s: hit after Delete", c.name)
			}
			_ = s.Set("a", "1")
			_ = s.Set("b", "2")
			if err := s.Clear(); err != nil {
				t.Fatalf("%s: Clear: %v", c.name, err)
			}
			if _, ok := s.Get("a"); ok {
				t.Errorf("%s: hit after Clear", c.name)
			}
		})
	}
}

func TestBackendFromEnvDefault(t *testing.T) {
	t.Setenv("CLOUDNAV_CACHE_BACKEND", "")
	t.Setenv("CLOUDNAV_CACHE", t.TempDir())
	b := BackendFromEnv()
	defer func() { _ = b.Close() }()
	if _, ok := b.(*SQLiteBackend); !ok {
		t.Errorf("default backend = %T, want *SQLiteBackend", b)
	}
}

func TestBackendFromEnvSelectsSQLite(t *testing.T) {
	t.Setenv("CLOUDNAV_CACHE_BACKEND", "sqlite")
	t.Setenv("CLOUDNAV_CACHE", t.TempDir())
	b := BackendFromEnv()
	defer func() { _ = b.Close() }()
	if _, ok := b.(*SQLiteBackend); !ok {
		t.Errorf("CLOUDNAV_CACHE_BACKEND=sqlite resolved to %T, want *SQLiteBackend", b)
	}
}

func TestBackendFromEnvSelectsJSON(t *testing.T) {
	for _, v := range []string{"json", "files", "file", "off"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("CLOUDNAV_CACHE_BACKEND", v)
			t.Setenv("CLOUDNAV_CACHE", t.TempDir())
			b := BackendFromEnv()
			defer func() { _ = b.Close() }()
			if _, ok := b.(*JSONBackend); !ok {
				t.Errorf("CLOUDNAV_CACHE_BACKEND=%q resolved to %T, want *JSONBackend", v, b)
			}
		})
	}
}
