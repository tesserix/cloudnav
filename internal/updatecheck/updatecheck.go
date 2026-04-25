// Package updatecheck polls the public GitHub releases feed for a newer
// cloudnav tag than the one baked into this binary. It's intentionally
// network-light: a single HTTP call to /releases/latest, a short
// timeout, a disk cache keyed by a 6-hour TTL so repeated runs don't
// hammer the API. Failures are silent — the header simply falls back
// to the quiet state when we can't reach GitHub.
package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tesserix/cloudnav/internal/cache"
)

// updateCheckBucket is the cache.Store bucket the update-check
// payload lives under. One row per process is enough — we only ever
// look up "the latest known release", keyed by `updateCheckKey`.
const (
	updateCheckBucket = "update-check"
	updateCheckKey    = "latest"
)

// store returns the cache.Store handle for the update-check payload.
// Backed by the process-wide cache.Shared() backend (default SQLite
// since 0.22.28) so the cached release lives in the same cloudnav.db
// as cost / pim / rgraph / azure-root.
func store() *cache.Store[cachedPayload] {
	return cache.NewStoreWithBackend[cachedPayload](
		cache.Shared(), updateCheckBucket, cacheStaleAfter,
	)
}

// Repo is the GitHub slug cloudnav releases are published under. Kept
// as a var rather than a const so forks / mirrors can patch it at build
// time with -ldflags -X.
var Repo = "tesserix/cloudnav"

// pollInterval is how often cloudnav contacts the GitHub releases API.
// Anonymous requests are capped at 60/hour per IP — in practice a user
// launching cloudnav a few times a day has no reason to poll on every
// start. One hour is the sweet spot: new releases surface within an
// hour of publishing, but a restart loop can't burn the quota.
const pollInterval = 1 * time.Hour

// cacheStaleAfter bounds how long a stale cache entry stays useful as
// a fallback when GitHub itself is unreachable. Longer than
// pollInterval — if the user is offline for a day we still render the
// last-known-good check instead of going quiet.
const cacheStaleAfter = 24 * time.Hour

// Result is the outcome of a single update check. Zero-value (Latest
// empty) means "couldn't resolve" — the caller should render the quiet
// header state rather than a prompt.
type Result struct {
	Latest    string    // e.g. "v1.4.0"
	Current   string    // e.g. "v1.3.2" (as passed in)
	Available bool      // true when Latest is strictly newer than Current
	ReleaseAt time.Time // publish time of the latest release
	URL       string    // canonical release URL (for browser fallback)
	Err       error     // populated on hard failures; caller treats as quiet
}

type cachedPayload struct {
	FetchedAt time.Time `json:"fetched_at"`
	Latest    string    `json:"latest"`
	URL       string    `json:"url"`
	ReleaseAt time.Time `json:"release_at"`
}

// Check returns the newest published release tag compared against
// current. Cache-first: reuses the cached result if it's within
// pollInterval, otherwise polls GitHub and refreshes the cache. On
// network / API failure falls back to the stale cache so the banner
// doesn't go quiet when the user is offline.
func Check(ctx context.Context, current string) Result {
	return runCheck(ctx, current, false)
}

// CheckForce bypasses the poll-interval cache and always hits GitHub.
// Used when the user presses U to explicitly re-check — they've
// already seen the cached answer and want us to look again.
func CheckForce(ctx context.Context, current string) Result {
	return runCheck(ctx, current, true)
}

func runCheck(ctx context.Context, current string, force bool) Result {
	if strings.TrimSpace(current) == "" {
		current = "dev"
	}
	cached, cachedOK := readCache()
	if !force && cachedOK && time.Since(cached.FetchedAt) < pollInterval {
		return finalise(current, cached.Latest, cached.URL, cached.ReleaseAt, nil)
	}
	latest, url, publishedAt, err := fetchLatest(ctx)
	if err == nil {
		writeCache(cachedPayload{FetchedAt: time.Now(), Latest: latest, URL: url, ReleaseAt: publishedAt})
		return finalise(current, latest, url, publishedAt, nil)
	}
	if cachedOK {
		return finalise(current, cached.Latest, cached.URL, cached.ReleaseAt, nil)
	}
	return Result{Current: current, Err: err}
}

func finalise(current, latest, url string, releaseAt time.Time, err error) Result {
	return Result{
		Current:   current,
		Latest:    latest,
		URL:       url,
		ReleaseAt: releaseAt,
		Available: IsNewer(latest, current),
		Err:       err,
	}
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
}

func fetchLatest(ctx context.Context) (string, string, time.Time, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", time.Time{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "cloudnav-updatecheck")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", time.Time{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return "", "", time.Time{}, errors.New("no release published yet")
	}
	if resp.StatusCode == http.StatusForbidden &&
		resp.Header.Get("X-RateLimit-Remaining") == "0" {
		return "", "", time.Time{}, errors.New("github: rate limit hit — check will retry on next poll")
	}
	if resp.StatusCode >= 400 {
		return "", "", time.Time{}, fmt.Errorf("github: status %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", time.Time{}, fmt.Errorf("decode release: %w", err)
	}
	if rel.Draft || rel.Prerelease {
		return "", "", time.Time{}, errors.New("only a draft/prerelease is published")
	}
	return rel.TagName, rel.HTMLURL, rel.PublishedAt, nil
}

// IsNewer reports whether latest is strictly greater than current using
// a best-effort semver-ish compare. Handles optional "v" prefix, common
// N.N.N form, and falls back to string compare when either side is
// non-numeric (e.g. Current == "dev" during local development).
func IsNewer(latest, current string) bool {
	latest = strings.TrimSpace(latest)
	current = strings.TrimSpace(current)
	if latest == "" || current == "" {
		return false
	}
	if latest == current {
		return false
	}
	if current == "dev" || current == "unknown" {
		// Dev builds are treated as "older than any real release" so the
		// prompt shows up when you're running a local build against a
		// tagged one — useful feedback while iterating.
		return true
	}
	l := parseSemver(latest)
	c := parseSemver(current)
	if l == nil || c == nil {
		// Fall back to plain string comparison — better than silently
		// hiding the prompt when the tag format is unusual.
		return latest > current
	}
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parseSemver(v string) []int {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	// Drop any "-rc.1" / "+meta" suffix.
	for _, sep := range []string{"-", "+"} {
		if idx := strings.Index(v, sep); idx >= 0 {
			v = v[:idx]
		}
	}
	parts := strings.Split(v, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return nil
	}
	out := make([]int, 3)
	for i := 0; i < 3; i++ {
		if i >= len(parts) {
			break
		}
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return nil
		}
		out[i] = n
	}
	return out
}

func readCache() (cachedPayload, bool) {
	out, ok := store().Get(updateCheckKey)
	if !ok {
		return cachedPayload{}, false
	}
	// cache.Store enforces TTL via cacheStaleAfter, but the
	// pollInterval check (FetchedAt) lives in Check() — that's the
	// short freshness window for "should we hit the network at all".
	return out, true
}

func writeCache(p cachedPayload) {
	_ = store().Set(updateCheckKey, p)
}

// ClearCache drops the local update-check cache so the next Check() is
// guaranteed to hit GitHub. Used by the upgrade flow after a successful
// install so the "update available" banner goes away immediately.
func ClearCache() {
	_ = store().Delete(updateCheckKey)
}
