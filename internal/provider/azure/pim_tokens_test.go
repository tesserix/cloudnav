package azure

import (
	"testing"
	"time"
)

func TestParseAzExpiryUnixSeconds(t *testing.T) {
	// Typical modern az output: expires_on is unix seconds as a string.
	got := parseAzExpiry("", "1713974400")
	want := time.Unix(1713974400, 0)
	if !got.Equal(want) {
		t.Errorf("parseAzExpiry(unix) = %v, want %v", got, want)
	}
}

func TestParseAzExpiryLocalString(t *testing.T) {
	// Legacy az output: local-time string in the expiresOn field.
	got := parseAzExpiry("2026-04-24 20:00:00.000000", "")
	if got.IsZero() {
		t.Error("parseAzExpiry(local) failed to parse fractional-second local time")
	}
}

func TestParseAzExpiryEmpty(t *testing.T) {
	if got := parseAzExpiry("", ""); !got.IsZero() {
		t.Errorf("parseAzExpiry(empty) = %v, want zero", got)
	}
}

func TestTokenCacheRoundTrip(t *testing.T) {
	// Isolate this test from any lingering global cache state.
	invalidateTokenCache()
	defer invalidateTokenCache()

	key := tokenCacheKey("tenant-a", armResource)
	tokenCacheMu.Lock()
	tokenCache[key] = cachedToken{token: "tkn-1", expiresAt: time.Now().Add(30 * time.Minute)}
	tokenCacheMu.Unlock()

	tokenCacheMu.Lock()
	entry, ok := tokenCache[key]
	tokenCacheMu.Unlock()

	if !ok || entry.token != "tkn-1" {
		t.Errorf("cached entry not retrievable: %+v ok=%v", entry, ok)
	}
}

func TestInvalidateTokenCache(t *testing.T) {
	tokenCacheMu.Lock()
	tokenCache["x|y"] = cachedToken{token: "z", expiresAt: time.Now().Add(time.Hour)}
	tokenCacheMu.Unlock()

	invalidateTokenCache()

	tokenCacheMu.Lock()
	n := len(tokenCache)
	tokenCacheMu.Unlock()

	if n != 0 {
		t.Errorf("after invalidate, cache size = %d, want 0", n)
	}
}

func TestTrimAPIErrExtractsMessage(t *testing.T) {
	raw := []byte(`{"error":{"code":"AuthorizationFailed","message":"The client does not have authorization to perform action 'Microsoft.Authorization/roleAssignments/write'."}}`)
	got := trimAPIErr(raw)
	if got != "AuthorizationFailed: The client does not have authorization to perform action 'Microsoft.Authorization/roleAssignments/write'." {
		t.Errorf("trimAPIErr returned %q", got)
	}
}

func TestTrimAPIErrFallsBackOnNonJSON(t *testing.T) {
	raw := []byte("Not a JSON envelope — just a string")
	got := trimAPIErr(raw)
	if got != "Not a JSON envelope — just a string" {
		t.Errorf("trimAPIErr fallback = %q", got)
	}
}

func TestTrimAPIErrHandlesEmpty(t *testing.T) {
	if got := trimAPIErr(nil); got != "(no error body)" {
		t.Errorf("trimAPIErr(nil) = %q", got)
	}
	if got := trimAPIErr([]byte("")); got != "(no error body)" {
		t.Errorf("trimAPIErr('') = %q", got)
	}
}
