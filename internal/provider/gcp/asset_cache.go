package gcp

import (
	"sort"
	"strings"
	"time"

	"github.com/tesserix/cloudnav/internal/cache"
	"github.com/tesserix/cloudnav/internal/provider"
)

// gcpAssetCacheTTL is intentionally shorter than gcpRootCacheTTL —
// resources churn faster than projects, and the TUI's `r` refresh
// is a one-keystroke escape hatch for the rare case where 5 min is
// too stale.
const gcpAssetCacheTTL = 5 * time.Minute

// gcpAssetCacheStore returns the cache.Store handle for per-project
// asset enumeration. Same pattern as the Azure rgraph cache.
func gcpAssetCacheStore() *cache.Store[[]provider.Node] {
	return cache.NewStoreWithBackend[[]provider.Node](
		cache.Shared(), "gcp-assets", gcpAssetCacheTTL,
	)
}

// gcpAssetCacheKey returns a deterministic cache key for a
// (project, asset-types) drill. Asset types are sorted so the
// caller passing them in different orders still hits the same row.
// projectID comes first so the SQLite key prefix is human-readable
// when scanning the table.
func gcpAssetCacheKey(projectID, assetTypes string) string {
	if assetTypes == "" {
		return projectID + "@*"
	}
	parts := strings.Split(assetTypes, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	sort.Strings(parts)
	return projectID + "@" + strings.Join(parts, ",")
}

// readAssetCache returns a cached drill result if one is present.
// Empty slice + true means the project legitimately has zero
// assets (still a cache hit — saves the round trip).
func readAssetCache(projectID, assetTypes string) ([]provider.Node, bool) {
	if gcpCacheDisabled() {
		return nil, false
	}
	return gcpAssetCacheStore().Get(gcpAssetCacheKey(projectID, assetTypes))
}

// writeAssetCache persists a drill result for future drills inside
// the TTL window. Best-effort.
func writeAssetCache(projectID, assetTypes string, nodes []provider.Node) {
	if gcpCacheDisabled() {
		return
	}
	_ = gcpAssetCacheStore().Set(gcpAssetCacheKey(projectID, assetTypes), nodes)
}
