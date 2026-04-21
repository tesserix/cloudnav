package azure

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tesserix/cloudnav/internal/provider"
)

// isolatedCache points the cache to a per-test temp dir and clears the opt-out
// env var so tests exercise the real caching code paths without clobbering a
// developer's real cache file.
func isolatedCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(envCacheDir, dir)
	t.Setenv(envNoCache, "")
	t.Setenv(envCacheTTL, "")
	return dir
}

func sampleNodes() []provider.Node {
	return []provider.Node{
		{
			ID:   "/subscriptions/aaaa-1111",
			Name: "Acme-Prod",
			Kind: provider.KindSubscription,
			Meta: map[string]string{"tenantId": "t-1", "user": "alice@example.com"},
		},
		{
			ID:   "/subscriptions/bbbb-2222",
			Name: "Acme-Dev",
			Kind: provider.KindSubscription,
			Meta: map[string]string{"tenantId": "t-1", "user": "alice@example.com"},
		},
	}
}

func TestRootDiskCacheRoundtrip(t *testing.T) {
	isolatedCache(t)
	nodes := sampleNodes()
	tenants := map[string]string{"t-1": "Acme Corp"}
	subTenants := map[string]string{
		"/subscriptions/aaaa-1111": "t-1",
		"/subscriptions/bbbb-2222": "t-1",
	}
	writeRootDiskCache(nodes, tenants, subTenants)

	got, ok := readRootDiskCache()
	if !ok {
		t.Fatal("expected cache hit after write")
	}
	if len(got.Nodes) != len(nodes) {
		t.Fatalf("nodes: got %d, want %d", len(got.Nodes), len(nodes))
	}
	if got.Nodes[0].Name != "Acme-Prod" {
		t.Errorf("first node name = %q", got.Nodes[0].Name)
	}
	if got.Tenants["t-1"] != "Acme Corp" {
		t.Errorf("tenant name lost: %v", got.Tenants)
	}
	if got.SubTenants["/subscriptions/aaaa-1111"] != "t-1" {
		t.Errorf("sub tenants lost: %v", got.SubTenants)
	}
}

func TestRootDiskCacheMissingFile(t *testing.T) {
	isolatedCache(t)
	if _, ok := readRootDiskCache(); ok {
		t.Error("empty dir should miss")
	}
}

func TestRootDiskCacheCorrupted(t *testing.T) {
	dir := isolatedCache(t)
	p := filepath.Join(dir, "azure-root.json")
	if err := os.WriteFile(p, []byte("not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := readRootDiskCache(); ok {
		t.Error("corrupted payload should miss, not panic")
	}
	// The reader should have cleaned up the bad file so subsequent runs
	// don't keep paying the decode cost.
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("corrupted cache file should be removed, stat err = %v", err)
	}
}

func TestRootDiskCacheTTLExpiry(t *testing.T) {
	isolatedCache(t)
	t.Setenv(envCacheTTL, "50ms")

	writeRootDiskCache(sampleNodes(), nil, nil)

	// Fresh: should hit.
	if _, ok := readRootDiskCache(); !ok {
		t.Fatal("expected fresh cache to hit")
	}

	time.Sleep(80 * time.Millisecond)

	if _, ok := readRootDiskCache(); ok {
		t.Error("expected cache to miss after TTL expired")
	}
}

func TestRootDiskCacheVersionMismatch(t *testing.T) {
	dir := isolatedCache(t)
	// Write a payload with a clearly-wrong version.
	stale := rootCacheFile{
		Version:   rootDiskCacheVersion + 99,
		CreatedAt: time.Now(),
		Nodes:     sampleNodes(),
	}
	data, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "azure-root.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := readRootDiskCache(); ok {
		t.Error("expected version-mismatched cache to miss")
	}
}

func TestRootDiskCacheDisabled(t *testing.T) {
	isolatedCache(t)
	t.Setenv(envNoCache, "1")
	writeRootDiskCache(sampleNodes(), nil, nil)
	if _, ok := readRootDiskCache(); ok {
		t.Error("CLOUDNAV_NO_CACHE should force a cache miss")
	}
}

func TestRootDiskCacheRemove(t *testing.T) {
	isolatedCache(t)
	writeRootDiskCache(sampleNodes(), nil, nil)
	if _, ok := readRootDiskCache(); !ok {
		t.Fatal("precondition: cache should exist")
	}
	removeRootDiskCache()
	if _, ok := readRootDiskCache(); ok {
		t.Error("removeRootDiskCache should make the next read miss")
	}
	// Removing again is a no-op, not an error.
	removeRootDiskCache()
}

func TestCacheTTLOverride(t *testing.T) {
	t.Setenv(envCacheTTL, "2m30s")
	if got := cacheTTL(); got != 2*time.Minute+30*time.Second {
		t.Errorf("cacheTTL = %s", got)
	}
	t.Setenv(envCacheTTL, "bogus")
	if got := cacheTTL(); got != rootDiskCacheTTL {
		t.Errorf("unparseable TTL should fall back to default, got %s", got)
	}
	t.Setenv(envCacheTTL, "0")
	if got := cacheTTL(); got != rootDiskCacheTTL {
		t.Errorf("non-positive TTL should fall back to default, got %s", got)
	}
}

func TestCacheDisabledEnv(t *testing.T) {
	cases := map[string]bool{
		"":      false,
		"0":     false,
		"false": false,
		"1":     true,
		"true":  true,
		"yes":   true,
	}
	for v, want := range cases {
		t.Setenv(envNoCache, v)
		if got := cacheDisabled(); got != want {
			t.Errorf("cacheDisabled(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestAzureRootUsesMemCache(t *testing.T) {
	isolatedCache(t)
	a := New()
	a.writeRootMem(sampleNodes())

	// Root() should hit the memory cache without ever touching az.
	got := a.readRootMem()
	if len(got) != 2 {
		t.Fatalf("mem cache miss: got %d nodes", len(got))
	}
	if got[0].Name != "Acme-Prod" {
		t.Errorf("first name = %q", got[0].Name)
	}

	// Mutating the returned slice must not corrupt the cached copy.
	got[0].Name = "MUTATED"
	fresh := a.readRootMem()
	if fresh[0].Name != "Acme-Prod" {
		t.Errorf("mem cache should return defensive copies, got %q", fresh[0].Name)
	}
}

func TestAzureInvalidateRootCacheClearsBoth(t *testing.T) {
	isolatedCache(t)
	a := New()
	a.writeRootMem(sampleNodes())
	writeRootDiskCache(sampleNodes(), nil, nil)

	a.InvalidateRootCache()

	if got := a.readRootMem(); got != nil {
		t.Errorf("mem cache should be cleared, got %d nodes", len(got))
	}
	if _, ok := readRootDiskCache(); ok {
		t.Error("disk cache should be cleared")
	}
}

func TestHydrateFromCachePopulatesMaps(t *testing.T) {
	a := New()
	a.hydrateFromCache(&rootCacheFile{
		Nodes:      sampleNodes(),
		Tenants:    map[string]string{"t-1": "Acme Corp"},
		SubTenants: map[string]string{"/subscriptions/aaaa-1111": "t-1"},
	})
	if a.subName("/subscriptions/aaaa-1111") != "Acme-Prod" {
		t.Errorf("subs map not hydrated: %q", a.subName("/subscriptions/aaaa-1111"))
	}
	if a.tenantName("t-1") != "Acme Corp" {
		t.Errorf("tenants map not hydrated: %q", a.tenantName("t-1"))
	}
}
