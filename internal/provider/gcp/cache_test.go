package gcp

import (
	"testing"
	"time"

	"github.com/tesserix/cloudnav/internal/cache"
	"github.com/tesserix/cloudnav/internal/provider"
)

// gcpCacheBackendForTests routes the gcp cache stores at a fresh
// SQLite file under t.TempDir so each test is isolated. cache.Shared
// is a process-wide singleton; we set CLOUDNAV_CACHE_BACKEND=json
// (a per-test temp dir) here so the JSONBackend handles the
// per-test dir cleanly without coordinating across tests.
func gcpCacheBackendForTests(t *testing.T) {
	t.Helper()
	t.Setenv("CLOUDNAV_CACHE_BACKEND", "json")
	t.Setenv("CLOUDNAV_CACHE", t.TempDir())
}

func TestGCPRootCacheRoundTrip(t *testing.T) {
	gcpCacheBackendForTests(t)
	want := []provider.Node{
		{ID: "p-prod", Name: "prod", Kind: provider.KindProject, State: "ACTIVE"},
		{ID: "p-dev", Name: "dev", Kind: provider.KindProject, State: "ACTIVE"},
	}
	writeRootDiskCacheGCP("", want)
	got, ok := readRootDiskCacheGCP("")
	if !ok {
		t.Fatal("Get miss after Set")
	}
	if len(got) != 2 || got[0].ID != "p-prod" {
		t.Errorf("round-trip lost data: %+v", got)
	}
}

func TestGCPRootCacheOrgIsolation(t *testing.T) {
	gcpCacheBackendForTests(t)
	writeRootDiskCacheGCP("org-A", []provider.Node{{ID: "p-A"}})
	if _, ok := readRootDiskCacheGCP("org-B"); ok {
		t.Error("cache hit across different orgs — must be isolated")
	}
	if _, ok := readRootDiskCacheGCP(""); ok {
		t.Error("cache hit when reading default after org-A write")
	}
}

func TestGCPRootCacheTTL(t *testing.T) {
	gcpCacheBackendForTests(t)
	t.Setenv(gcpEnvCacheTTL, "1ms")
	writeRootDiskCacheGCP("", []provider.Node{{ID: "p"}})
	time.Sleep(10 * time.Millisecond)
	if _, ok := readRootDiskCacheGCP(""); ok {
		t.Error("cache hit past TTL")
	}
}

func TestGCPRootCacheDisabled(t *testing.T) {
	gcpCacheBackendForTests(t)
	t.Setenv(gcpEnvNoCache, "1")
	writeRootDiskCacheGCP("", []provider.Node{{ID: "p"}})
	if _, ok := readRootDiskCacheGCP(""); ok {
		t.Error("CLOUDNAV_GCP_NO_CACHE should force a miss")
	}
}

func TestGCPRootCacheRemove(t *testing.T) {
	gcpCacheBackendForTests(t)
	writeRootDiskCacheGCP("", []provider.Node{{ID: "p"}})
	if _, ok := readRootDiskCacheGCP(""); !ok {
		t.Fatal("precondition: cache should be populated")
	}
	removeRootDiskCacheGCP()
	if _, ok := readRootDiskCacheGCP(""); ok {
		t.Error("InvalidateRootCache should drop the cached row")
	}
}

func TestGCPAssetCacheKeyOrderInvariant(t *testing.T) {
	a := gcpAssetCacheKey("p-prod", "compute.googleapis.com/Instance,storage.googleapis.com/Bucket")
	b := gcpAssetCacheKey("p-prod", "storage.googleapis.com/Bucket,compute.googleapis.com/Instance")
	if a != b {
		t.Errorf("asset cache key differs by type order:\n  ascii = %q\n  reversed = %q", a, b)
	}
}

func TestGCPAssetCacheKeyVariesByProject(t *testing.T) {
	if gcpAssetCacheKey("p-A", "x") == gcpAssetCacheKey("p-B", "x") {
		t.Error("asset cache key should differ across projects")
	}
}

func TestGCPAssetCacheRoundTrip(t *testing.T) {
	gcpCacheBackendForTests(t)
	want := []provider.Node{
		{ID: "/projects/p/instances/foo", Name: "foo", Kind: provider.KindResource},
	}
	writeAssetCache("p", "compute.googleapis.com/Instance", want)
	got, ok := readAssetCache("p", "compute.googleapis.com/Instance")
	if !ok {
		t.Fatal("Get miss after Set")
	}
	if len(got) != 1 || got[0].Name != "foo" {
		t.Errorf("round-trip lost data: %+v", got)
	}
}

// TestSharedBackendIsSQLiteByDefault — sanity-check the path the
// user actually exercises in production. cache.Shared() must
// resolve to *SQLiteBackend without env overrides.
func TestSharedBackendIsSQLiteByDefault(t *testing.T) {
	t.Setenv("CLOUDNAV_CACHE_BACKEND", "")
	t.Setenv("CLOUDNAV_CACHE", t.TempDir())
	// Use BackendFromEnv directly because cache.Shared caches a
	// once.Do that may have been resolved earlier in a different
	// test goroutine.
	be := cache.BackendFromEnv()
	defer func() { _ = be.Close() }()
	if _, ok := be.(*cache.SQLiteBackend); !ok {
		t.Errorf("cache backend default = %T, want *SQLiteBackend", be)
	}
}
