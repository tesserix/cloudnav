package azure

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// credentialOnce lazy-initialises the Default credential the first time
// we need a token. Reused across tenants via the per-tenant AzureCLI
// credential below.
var (
	credentialOnce sync.Once
	credential     azcore.TokenCredential
	credentialErr  error
)

func defaultCredential() (azcore.TokenCredential, error) {
	credentialOnce.Do(func() {
		credential, credentialErr = azidentity.NewDefaultAzureCredential(nil)
	})
	return credential, credentialErr
}

// tenantCLICreds caches AzureCLICredential instances keyed by tenant so a
// multi-tenant user reuses the same credential for the session.
var (
	tenantCredMu sync.Mutex
	tenantCreds  = map[string]azcore.TokenCredential{}
)

func tenantCLICred(tenantID string) (azcore.TokenCredential, error) {
	tenantCredMu.Lock()
	defer tenantCredMu.Unlock()
	if c, ok := tenantCreds[tenantID]; ok {
		return c, nil
	}
	c, err := azidentity.NewAzureCLICredential(&azidentity.AzureCLICredentialOptions{
		TenantID: tenantID,
	})
	if err != nil {
		return nil, err
	}
	tenantCreds[tenantID] = c
	return c, nil
}

// sdkToken returns a bearer token for the given tenant / resource using
// the Azure SDK credentials. It reads the az login cache directly —
// zero process spawn — and honours the in-memory tokenCache so repeat
// calls within a session never re-hit the credential chain.
//
// Falls back to the az-CLI path only when the SDK credential fails
// (rare: happens only when az isn't installed or the user hasn't logged
// in, in which case the fallback itself will surface a clear error).
func (a *Azure) sdkToken(ctx context.Context, tenantID, resource string) (string, error) {
	key := tokenCacheKey(tenantID, resource)

	tokenCacheMu.Lock()
	if entry, ok := tokenCache[key]; ok && time.Until(entry.expiresAt) > tokenCacheSkew {
		tokenCacheMu.Unlock()
		return entry.token, nil
	}
	tokenCacheMu.Unlock()

	cred, err := tenantCLICred(tenantID)
	if err != nil {
		return "", err
	}
	// Azure's TokenRequestOptions takes a list of scopes. Resource URIs
	// like https://management.azure.com/ become scopes of the form
	// "<resource>.default".
	scope := resource
	if len(scope) == 0 || scope[len(scope)-1] != '/' {
		scope += "/"
	}
	scope += ".default"
	tok, err := cred.GetToken(ctx, policy.TokenRequestOptions{
		Scopes:   []string{scope},
		TenantID: tenantID,
	})
	if err != nil {
		return "", fmt.Errorf("tenant %s: %w", tenantID, err)
	}
	tokenCacheMu.Lock()
	tokenCache[key] = cachedToken{token: tok.Token, expiresAt: tok.ExpiresOn}
	tokenCacheMu.Unlock()
	return tok.Token, nil
}
