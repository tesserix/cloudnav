package tui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/tesserix/cloudnav/internal/cache"
	"github.com/tesserix/cloudnav/internal/provider"
)

func TestRgraphCacheKeyOrderInvariant(t *testing.T) {
	subID := "sub-123"
	a := rgraphCacheKey(subID, []string{"rg-a", "rg-b", "rg-c"})
	b := rgraphCacheKey(subID, []string{"rg-c", "rg-a", "rg-b"})
	c := rgraphCacheKey(subID, []string{"rg-b", "rg-c", "rg-a"})
	if a != b || a != c {
		t.Errorf("cache key differs by selection order:\n  abc = %q\n  cab = %q\n  bca = %q", a, b, c)
	}
}

func TestRgraphCacheKeyVariesBySub(t *testing.T) {
	rgs := []string{"rg-a", "rg-b"}
	if rgraphCacheKey("sub-A", rgs) == rgraphCacheKey("sub-B", rgs) {
		t.Error("cache key should differ when subscription differs")
	}
}

func TestRgraphCacheKeyVariesByRGSet(t *testing.T) {
	if rgraphCacheKey("sub", []string{"rg-a"}) == rgraphCacheKey("sub", []string{"rg-a", "rg-b"}) {
		t.Error("cache key should differ when RG set differs")
	}
}

// TestRgraphCacheRoundTrip exercises the actual Store the model uses
// — round-tripping a populated drill result through a fresh SQLite
// backend and asserting Get returns the stored nodes byte-for-byte.
func TestRgraphCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	be, err := cache.NewSQLiteBackend(filepath.Join(dir, "rgraph.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = be.Close() })
	store := cache.NewStoreWithBackend[[]provider.Node](be, "rgraph", time.Minute)

	want := []provider.Node{
		{ID: "/subscriptions/x/rgs/y/vms/foo", Name: "foo", Kind: provider.KindResource, Location: "uksouth", Meta: map[string]string{"type": "Microsoft.Compute/virtualMachines"}},
		{ID: "/subscriptions/x/rgs/y/sa/bar", Name: "bar", Kind: provider.KindResource, Location: "uksouth", Meta: map[string]string{"type": "Microsoft.Storage/storageAccounts"}},
	}
	key := rgraphCacheKey("sub-X", []string{"rg-Y"})
	if err := store.Set(key, want); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok := store.Get(key)
	if !ok {
		t.Fatal("Get miss after Set")
	}
	if len(got) != len(want) {
		t.Fatalf("Get returned %d nodes, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i].ID || got[i].Name != want[i].Name {
			t.Errorf("node %d: got %+v, want %+v", i, got[i], want[i])
		}
		if got[i].Meta["type"] != want[i].Meta["type"] {
			t.Errorf("node %d meta[type]: got %q, want %q", i, got[i].Meta["type"], want[i].Meta["type"])
		}
	}
}

// TestRgraphCacheClearDropsEntries — the `r` refresh path calls
// Clear() so subsequent drills re-fetch. Guard the contract.
func TestRgraphCacheClearDropsEntries(t *testing.T) {
	dir := t.TempDir()
	be, err := cache.NewSQLiteBackend(filepath.Join(dir, "rgraph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = be.Close() })
	store := cache.NewStoreWithBackend[[]provider.Node](be, "rgraph", time.Minute)

	_ = store.Set("k1", []provider.Node{{Name: "a"}})
	_ = store.Set("k2", []provider.Node{{Name: "b"}})
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get("k1"); ok {
		t.Error("k1 should miss after Clear")
	}
	if _, ok := store.Get("k2"); ok {
		t.Error("k2 should miss after Clear")
	}
}
