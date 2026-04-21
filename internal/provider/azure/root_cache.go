package azure

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/tesserix/cloudnav/internal/provider"
)

// rootDiskCacheVersion bumps when the on-disk payload shape changes so old
// caches are ignored rather than silently mis-decoded.
const rootDiskCacheVersion = 1

// rootDiskCacheTTL is the max age at which a cached Root() result is served
// from disk. Kept short so a newly added or removed subscription shows up
// after one TTL window even without an explicit refresh. Override with
// CLOUDNAV_AZURE_CACHE_TTL (a Go duration string, e.g. "2m", "1h").
const rootDiskCacheTTL = 10 * time.Minute

// envNoCache disables both the in-memory and the disk cache when set to a
// truthy value. Useful for CI and for debugging apparent staleness.
const envNoCache = "CLOUDNAV_NO_CACHE"

// envCacheTTL lets operators tune the disk-cache TTL without recompiling.
const envCacheTTL = "CLOUDNAV_AZURE_CACHE_TTL"

// envCacheDir overrides the cache location (useful in tests).
const envCacheDir = "CLOUDNAV_CACHE_DIR"

type rootCacheFile struct {
	Version     int               `json:"version"`
	CreatedAt   time.Time         `json:"created_at"`
	Fingerprint string            `json:"fingerprint"`
	Nodes       []provider.Node   `json:"nodes"`
	Tenants     map[string]string `json:"tenants,omitempty"`
	SubTenants  map[string]string `json:"sub_tenants,omitempty"`
}

// cacheDisabled reports whether the user opted out of caching.
func cacheDisabled() bool {
	v := os.Getenv(envNoCache)
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		// Accept any non-empty non-"0" value as truthy so "1"/"true"/"yes" all
		// disable. Strict parse errors shouldn't silently re-enable caching.
		return v != "0"
	}
	return b
}

// cacheTTL returns the configured disk-cache TTL, falling back to the default
// when the env override is unset or unparseable.
func cacheTTL() time.Duration {
	v := os.Getenv(envCacheTTL)
	if v == "" {
		return rootDiskCacheTTL
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return rootDiskCacheTTL
	}
	return d
}

// rootCachePath returns the file path for the Azure Root() cache. Falls back
// to os.TempDir when UserCacheDir is unavailable so the cache still works in
// minimal environments (CI runners, scratch containers).
func rootCachePath() string {
	if v := os.Getenv(envCacheDir); v != "" {
		return filepath.Join(v, "azure-root.json")
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "cloudnav", "azure-root.json")
}

// azProfileFingerprint returns a stable short string derived from the Azure
// CLI's profile file. Login changes bump its mtime and size, which changes
// the fingerprint — that invalidates the cache automatically when the user
// runs `az login` / `az logout` / `az account set`.
//
// If the profile is unreadable (e.g. Azure CLI not installed), we return a
// constant placeholder so caching still works — we just lose the
// auto-invalidate-on-login property, which the TTL still bounds.
func azProfileFingerprint() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "nohome"
	}
	p := filepath.Join(home, ".azure", "azureProfile.json")
	info, err := os.Stat(p)
	if err != nil {
		return "noprofile"
	}
	h := sha256.New()
	h.Write([]byte(fmt.Sprintf("%d:%d:%s", info.Size(), info.ModTime().UnixNano(), info.Name())))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// readRootDiskCache returns a cached Root() payload if one exists, is fresh,
// and matches the current az profile fingerprint. All failure modes
// (missing, corrupted, stale, wrong fingerprint) return (nil, false) so the
// caller falls through to a live fetch.
func readRootDiskCache() (*rootCacheFile, bool) {
	if cacheDisabled() {
		return nil, false
	}
	p := rootCachePath()
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	var c rootCacheFile
	if err := json.Unmarshal(data, &c); err != nil {
		// Corrupted payload — best-effort cleanup so we don't keep re-reading it.
		_ = os.Remove(p)
		return nil, false
	}
	if c.Version != rootDiskCacheVersion {
		return nil, false
	}
	if time.Since(c.CreatedAt) > cacheTTL() {
		return nil, false
	}
	if c.Fingerprint != "" && c.Fingerprint != azProfileFingerprint() {
		return nil, false
	}
	return &c, true
}

// writeRootDiskCache persists a Root() result for future cold starts. Best
// effort: write failures are swallowed because caching is strictly an
// optimisation — we must never break the foreground fetch.
func writeRootDiskCache(nodes []provider.Node, tenants, subTenants map[string]string) {
	if cacheDisabled() {
		return
	}
	p := rootCachePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	payload := rootCacheFile{
		Version:     rootDiskCacheVersion,
		CreatedAt:   time.Now().UTC(),
		Fingerprint: azProfileFingerprint(),
		Nodes:       nodes,
		Tenants:     tenants,
		SubTenants:  subTenants,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	// Atomic-ish write so a crash mid-write doesn't leave a truncated file
	// that readRootDiskCache would treat as corrupted. Rename is atomic on the
	// same filesystem; errors here are non-fatal.
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
	}
}

// removeRootDiskCache wipes the cache file. Called by the explicit refresh
// path so the next Root() goes to the wire.
func removeRootDiskCache() {
	p := rootCachePath()
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		// Still best-effort, but don't leak errno through.
		return
	}
}
