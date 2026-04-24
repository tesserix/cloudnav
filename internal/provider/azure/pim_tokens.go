package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// tenantToken returns a bearer token for the Azure Management API audience.
func (a *Azure) tenantToken(ctx context.Context, tenantID string) (string, error) {
	return a.tenantTokenFor(ctx, tenantID, armResource)
}

// cachedToken is a single (tenant, audience) entry in the process-wide
// token cache. expiresAt is populated from az's response so we refresh
// well before Azure actually invalidates the token.
type cachedToken struct {
	token     string
	expiresAt time.Time
}

// tokenCache holds access tokens keyed by (tenant, audience). Azure
// tokens are valid for ~60 minutes; we reuse them up to a small safety
// margin before expiry so a PIM list across N tenants doesn't spawn N
// 'az account get-access-token' processes per source (ARM + Graph).
var (
	tokenCacheMu sync.Mutex
	tokenCache   = map[string]cachedToken{}
)

// tokenCacheKey combines tenant + audience into the cache key.
func tokenCacheKey(tenantID, resource string) string {
	return tenantID + "|" + resource
}

// tokenCacheSkew leaves us this much time before the actual expiry so a
// token we just handed out doesn't die mid-request on a slow network.
const tokenCacheSkew = 2 * time.Minute

// tenantTokenFor returns a bearer token for the given audience. First
// tries the Azure SDK credential (which reads the az login cache
// directly, no process spawn) and falls back to 'az account
// get-access-token' only when that fails. Cached per (tenant, audience)
// in memory until near expiry.
func (a *Azure) tenantTokenFor(ctx context.Context, tenantID, resource string) (string, error) {
	if tok, err := a.sdkToken(ctx, tenantID, resource); err == nil {
		return tok, nil
	}
	return a.tenantTokenViaCLI(ctx, tenantID, resource)
}

func (a *Azure) tenantTokenViaCLI(ctx context.Context, tenantID, resource string) (string, error) {
	key := tokenCacheKey(tenantID, resource)

	tokenCacheMu.Lock()
	if entry, ok := tokenCache[key]; ok && time.Until(entry.expiresAt) > tokenCacheSkew {
		tokenCacheMu.Unlock()
		return entry.token, nil
	}
	tokenCacheMu.Unlock()

	out, err := a.az.Run(ctx,
		"account", "get-access-token",
		"--tenant", tenantID,
		"--resource", resource,
		"-o", "json",
	)
	if err != nil {
		return "", err
	}
	var t struct {
		AccessToken string `json:"accessToken"`
		ExpiresOn   string `json:"expiresOn"`  // legacy local-time
		ExpiresOnT  string `json:"expires_on"` // unix seconds (newer az)
	}
	if err := json.Unmarshal(out, &t); err != nil {
		return "", err
	}
	if t.AccessToken == "" {
		return "", fmt.Errorf("empty token for tenant %s (audience %s)", tenantID, resource)
	}

	expiresAt := parseAzExpiry(t.ExpiresOn, t.ExpiresOnT)
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(50 * time.Minute)
	}
	tokenCacheMu.Lock()
	tokenCache[key] = cachedToken{token: t.AccessToken, expiresAt: expiresAt}
	tokenCacheMu.Unlock()

	return t.AccessToken, nil
}

// parseAzExpiry tries the two formats the Azure CLI has used. New
// versions emit expires_on as unix seconds (string); older versions
// emit expiresOn as a local-time string like "2026-04-24 20:00:00.000000".
// Returns the zero Time on parse failure — the caller then applies a
// fallback TTL.
func parseAzExpiry(localStr, unixStr string) time.Time {
	if unixStr != "" {
		if unix, err := parseInt64(unixStr); err == nil {
			return time.Unix(unix, 0)
		}
	}
	if localStr != "" {
		for _, layout := range []string{
			"2006-01-02 15:04:05.000000",
			"2006-01-02 15:04:05",
			time.RFC3339,
		} {
			if t, err := time.ParseInLocation(layout, localStr, time.Local); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

// parseInt64 is a tiny strconv.ParseInt wrapper that's easy to mock in
// tests without dragging in an extra import at the call sites.
func parseInt64(s string) (int64, error) {
	var n int64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid digit at %d: %q", i, c)
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}

// invalidateTokenCache drops all cached tokens. Called by ClearCache
// paths so a user who ran 'az logout' can get a fresh pull.
func invalidateTokenCache() {
	tokenCacheMu.Lock()
	tokenCache = map[string]cachedToken{}
	tokenCacheMu.Unlock()
}
