// AWS cache layer — mirrors the GCP / Azure pattern. Two buckets
// in cloudnav.db:
//
//   - aws-root      → account list (Organizations:ListAccounts or
//     STS GetCallerIdentity single-account fallback).
//   - aws-resources → per-region resource enumeration from the
//     Resource Groups Tagging API.
//
// Both honour CLOUDNAV_AWS_NO_CACHE for opt-out and
// CLOUDNAV_AWS_CACHE_TTL to override the defaults.
package aws

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

const (
	awsRootCacheVersion = 1

	// awsRootCacheTTL bounds the staleness of the cached account
	// list. 10 minutes matches the Azure / GCP root cache —
	// long enough to skip Organizations:ListAccounts on rapid
	// relaunches, short enough that newly-added accounts surface
	// within one TTL window.
	awsRootCacheTTL = 10 * time.Minute

	// awsResourcesCacheTTL bounds per-region resource staleness.
	// Resources churn faster than account membership, so 5 min
	// — same as gcp-assets and rgraph.
	awsResourcesCacheTTL = 5 * time.Minute

	awsEnvNoCache  = "CLOUDNAV_AWS_NO_CACHE"
	awsEnvCacheTTL = "CLOUDNAV_AWS_CACHE_TTL"
)

type awsRootCacheFile struct {
	Version     int             `json:"version"`
	CreatedAt   time.Time       `json:"created_at"`
	Fingerprint string          `json:"fingerprint"`
	Nodes       []provider.Node `json:"nodes"`
}

func awsRootCacheStore() *cache.Store[awsRootCacheFile] {
	return cache.NewStoreWithBackend[awsRootCacheFile](
		cache.Shared(), "aws-root", awsRootEffectiveTTL(),
	)
}

func awsResourcesCacheStore() *cache.Store[[]provider.Node] {
	return cache.NewStoreWithBackend[[]provider.Node](
		cache.Shared(), "aws-resources", awsResourcesCacheTTL,
	)
}

const awsRootCacheKey = "current"

func awsCacheDisabled() bool {
	v := os.Getenv(awsEnvNoCache)
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return v != "0"
	}
	return b
}

func awsRootEffectiveTTL() time.Duration {
	v := os.Getenv(awsEnvCacheTTL)
	if v == "" {
		return awsRootCacheTTL
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return awsRootCacheTTL
	}
	return d
}

// awsCredFingerprint returns a stable short string derived from the
// AWS CLI's config + credentials files plus the active profile env
// var. When the user runs `aws sso login` / `aws configure` /
// `export AWS_PROFILE=other`, the fingerprint changes and the
// cache invalidates automatically.
//
// Returns a constant placeholder when the AWS config can't be read
// (no ~/.aws, fresh container) so the cache still works — we just
// lose the auto-invalidate-on-login property, which the TTL still
// bounds.
func awsCredFingerprint() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "nohome"
	}
	candidates := []string{
		filepath.Join(home, ".aws", "config"),
		filepath.Join(home, ".aws", "credentials"),
		filepath.Join(home, ".aws", "sso", "cache"),
	}
	h := sha256.New()
	for _, p := range candidates {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		_, _ = fmt.Fprintf(h, "%s:%d:%d|", filepath.Base(p), info.Size(), info.ModTime().UnixNano())
	}
	// Profile env var also influences which creds the SDK picks
	// up, so it's part of the identity fingerprint.
	_, _ = fmt.Fprintf(h, "profile=%s|region=%s",
		os.Getenv("AWS_PROFILE"), os.Getenv("AWS_REGION"))
	sum := h.Sum(nil)
	if len(sum) == 0 {
		return "noprofile"
	}
	return hex.EncodeToString(sum)[:16]
}

func readRootCacheAWS() ([]provider.Node, bool) {
	if awsCacheDisabled() {
		return nil, false
	}
	c, ok := awsRootCacheStore().Get(awsRootCacheKey)
	if !ok {
		return nil, false
	}
	if c.Version != awsRootCacheVersion {
		return nil, false
	}
	if c.Fingerprint != "" && c.Fingerprint != awsCredFingerprint() {
		return nil, false
	}
	return c.Nodes, true
}

func writeRootCacheAWS(nodes []provider.Node) {
	if awsCacheDisabled() {
		return
	}
	payload := awsRootCacheFile{
		Version:     awsRootCacheVersion,
		CreatedAt:   time.Now().UTC(),
		Fingerprint: awsCredFingerprint(),
		Nodes:       nodes,
	}
	_ = awsRootCacheStore().Set(awsRootCacheKey, payload)
}

// InvalidateRootCache drops the cached account list. Mirrors the
// Azure / GCP same-named methods so the TUI's `r` refresh hits
// every cloud uniformly.
func (a *AWS) InvalidateRootCache() {
	_ = awsRootCacheStore().Delete(awsRootCacheKey)
}

// resourceCacheKey is the deterministic key for a per-region
// resource drill. Single-segment for now (just the region) — when
// we add tag filtering we'll fold the filter into the key the same
// way the GCP asset cache does for asset types.
func resourceCacheKey(region string) string {
	return region
}

func readResourcesCacheAWS(region string) ([]provider.Node, bool) {
	if awsCacheDisabled() {
		return nil, false
	}
	return awsResourcesCacheStore().Get(resourceCacheKey(region))
}

func writeResourcesCacheAWS(region string, nodes []provider.Node) {
	if awsCacheDisabled() {
		return
	}
	_ = awsResourcesCacheStore().Set(resourceCacheKey(region), nodes)
}
