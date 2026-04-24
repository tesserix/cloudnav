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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub slug cloudnav releases are published under. Kept
// as a var rather than a const so forks / mirrors can patch it at build
// time with -ldflags -X.
var Repo = "tesserix/cloudnav"

// cacheStaleAfter bounds how long a stale cache entry stays useful as a
// *fallback* when GitHub itself is unreachable. The cache isn't used to
// short-circuit the live call any more — every startup re-verifies with
// GitHub — so this only has to be long enough to cover an offline day.
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
// current. Always hits GitHub first so a newly-cut release is detected
// the next time cloudnav starts; the disk cache is only consulted as a
// fallback when the network call fails (flight mode, 503, API rate
// limit) so the header doesn't silently go quiet in offline situations.
func Check(ctx context.Context, current string) Result {
	if strings.TrimSpace(current) == "" {
		current = "dev"
	}
	latest, url, publishedAt, err := fetchLatest(ctx)
	if err == nil {
		writeCache(cachedPayload{FetchedAt: time.Now(), Latest: latest, URL: url, ReleaseAt: publishedAt})
		return finalise(current, latest, url, publishedAt, nil)
	}
	if cached, ok := readCache(); ok {
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

func cachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "cloudnav", "update-check.json")
}

func readCache() (cachedPayload, bool) {
	p := cachePath()
	if p == "" {
		return cachedPayload{}, false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return cachedPayload{}, false
	}
	var out cachedPayload
	if err := json.Unmarshal(data, &out); err != nil {
		return cachedPayload{}, false
	}
	if time.Since(out.FetchedAt) > cacheStaleAfter {
		return cachedPayload{}, false
	}
	return out, true
}

func writeCache(p cachedPayload) {
	path := cachePath()
	if path == "" {
		return
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	data, err := json.Marshal(p)
	if err != nil {
		return
	}
	// Atomic write: write to a temp file in the same directory, then rename.
	// Protects against torn writes when two goroutines race (and against
	// half-written JSON when the process is killed mid-write).
	tmp, err := os.CreateTemp(dir, ".updatecheck-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
	}
}

// ClearCache drops the local update-check cache so the next Check() is
// guaranteed to hit GitHub. Used by the upgrade flow after a successful
// install so the "update available" banner goes away immediately.
func ClearCache() {
	p := cachePath()
	if p == "" {
		return
	}
	_ = os.Remove(p)
}
