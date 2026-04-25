package gcp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/tesserix/cloudnav/internal/cache"
	"github.com/tesserix/cloudnav/internal/provider"
)

// Mirrors internal/provider/azure/root_cache.go — same bucket-per-cloud
// shape so the SQLite cache file holds one well-segregated row set
// per cloud's enumeration. The Azure side caches subscriptions; GCP
// caches the Root() result (projects, or folders when an org is set
// via CLOUDNAV_GCP_ORG).

const (
	gcpRootCacheVersion = 1
	// gcpRootDiskCacheTTL bounds how stale the cached enumeration may
	// be. 10 minutes — same as the Azure root TTL — balances "new
	// projects show up promptly" against "don't re-hit SearchProjects
	// every launch". Override with CLOUDNAV_GCP_CACHE_TTL.
	gcpRootDiskCacheTTL = 10 * time.Minute

	gcpEnvNoCache  = "CLOUDNAV_GCP_NO_CACHE"
	gcpEnvCacheTTL = "CLOUDNAV_GCP_CACHE_TTL"
)

type gcpRootCacheFile struct {
	Version     int             `json:"version"`
	CreatedAt   time.Time       `json:"created_at"`
	Fingerprint string          `json:"fingerprint"`
	Org         string          `json:"org,omitempty"` // "" for default flat-project mode
	Nodes       []provider.Node `json:"nodes"`
}

// gcpRootCacheStore returns the cache.Store handle for GCP Root()
// snapshots. Backed by the process-wide cache.Shared() (default
// SQLite since 0.22.28) so the rows live in cloudnav.db alongside
// every other cache.
func gcpRootCacheStore() *cache.Store[gcpRootCacheFile] {
	return cache.NewStoreWithBackend[gcpRootCacheFile](
		cache.Shared(), "gcp-root", gcpRootCacheTTL(),
	)
}

const gcpRootCacheKey = "current"

func gcpCacheDisabled() bool {
	v := os.Getenv(gcpEnvNoCache)
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return v != "0"
	}
	return b
}

func gcpRootCacheTTL() time.Duration {
	v := os.Getenv(gcpEnvCacheTTL)
	if v == "" {
		return gcpRootDiskCacheTTL
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return gcpRootDiskCacheTTL
	}
	return d
}

// gcloudCredFingerprint returns a stable short string derived from
// gcloud's active config files. When the user runs `gcloud auth
// login` / `gcloud config set account ...` / `gcloud config
// configurations activate <other>`, the underlying files change
// and the fingerprint changes — that auto-invalidates the cache so
// the user doesn't see stale projects from a previous identity.
//
// Returns a constant placeholder when the gcloud config can't be
// read (gcloud not installed, fresh container) so the cache still
// works — we just lose the auto-invalidate-on-login property,
// which the TTL still bounds.
func gcloudCredFingerprint() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "nohome"
	}
	candidates := []string{
		// Active config selector
		filepath.Join(home, ".config", "gcloud", "active_config"),
		// ADC credentials (changes on `gcloud auth application-default login`)
		filepath.Join(home, ".config", "gcloud", "application_default_credentials.json"),
		// Per-user credentials.db (changes on `gcloud auth login`)
		filepath.Join(home, ".config", "gcloud", "credentials.db"),
	}
	h := sha256.New()
	for _, p := range candidates {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		_, _ = fmt.Fprintf(h, "%s:%d:%d|", filepath.Base(p), info.Size(), info.ModTime().UnixNano())
	}
	sum := h.Sum(nil)
	if len(sum) == 0 {
		return "noprofile"
	}
	return hex.EncodeToString(sum)[:16]
}

// readRootDiskCacheGCP returns a cached Root() payload if it exists,
// is fresh, matches the active org (when set), and matches the
// current gcloud fingerprint. Any failure mode returns
// (nil, false) so the caller falls through to the live SDK / CLI
// fetch.
func readRootDiskCacheGCP(org string) ([]provider.Node, bool) {
	if gcpCacheDisabled() {
		return nil, false
	}
	c, ok := gcpRootCacheStore().Get(gcpRootCacheKey)
	if !ok {
		return nil, false
	}
	if c.Version != gcpRootCacheVersion {
		return nil, false
	}
	if c.Org != org {
		// Different org context — must re-fetch so a switch from
		// org-mode to flat-projects-mode (or between two orgs)
		// doesn't read stale rows.
		return nil, false
	}
	if c.Fingerprint != "" && c.Fingerprint != gcloudCredFingerprint() {
		return nil, false
	}
	return c.Nodes, true
}

// writeRootDiskCacheGCP persists a Root() result for future cold
// starts. Best-effort: write failures are swallowed because caching
// is strictly an optimisation.
func writeRootDiskCacheGCP(org string, nodes []provider.Node) {
	if gcpCacheDisabled() {
		return
	}
	payload := gcpRootCacheFile{
		Version:     gcpRootCacheVersion,
		CreatedAt:   time.Now().UTC(),
		Fingerprint: gcloudCredFingerprint(),
		Org:         org,
		Nodes:       nodes,
	}
	_ = gcpRootCacheStore().Set(gcpRootCacheKey, payload)
}

// removeRootDiskCacheGCP wipes the cache row. Used by the explicit
// refresh path (`r` key in the TUI when at the cloud root).
func removeRootDiskCacheGCP() {
	_ = gcpRootCacheStore().Delete(gcpRootCacheKey)
}

// InvalidateRootCache drops the cached enumeration so the next
// Root() goes to the wire. Provided as a public method to mirror
// Azure's same-named method on `*Azure` — the TUI calls it on `r`
// at the cloud root.
func (g *GCP) InvalidateRootCache() {
	removeRootDiskCacheGCP()
}
