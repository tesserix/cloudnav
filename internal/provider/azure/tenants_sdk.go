package azure

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

// fetchTenantsSDK returns {tenantId: displayName} via the ARM /tenants
// endpoint using an SDK-minted token (no az process spawn). ok=false
// means the caller should fall back to az rest /tenants.
func (a *Azure) fetchTenantsSDK(ctx context.Context) (map[string]string, bool) {
	cred, err := defaultCredential()
	if err != nil {
		return nil, false
	}
	tok, err := cred.GetToken(ctx, armTokenOptions())
	if err != nil {
		return nil, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://management.azure.com/tenants?api-version=2022-09-01", nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+tok.Token)
	resp, err := doWithRetry(req)
	if err != nil {
		return nil, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, false
	}
	var env struct {
		Value []struct {
			TenantID    string `json:"tenantId"`
			DisplayName string `json:"displayName"`
		} `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, false
	}
	out := make(map[string]string, len(env.Value))
	for _, t := range env.Value {
		if t.TenantID != "" {
			out[t.TenantID] = t.DisplayName
		}
	}
	return out, true
}

// armTokenOptions is the scope request the LoggedIn probe uses.
func armTokenOptions() policy.TokenRequestOptions {
	return policy.TokenRequestOptions{Scopes: []string{armResource + ".default"}}
}

// tenantForSubSDK looks up the tenant a subscription belongs to via the
// ARM /subscriptions/<id> endpoint — same URL the az CLI hits, but using
// an SDK-minted bearer token instead of a process spawn.
func (a *Azure) tenantForSubSDK(ctx context.Context, subID string) string {
	cred, err := defaultCredential()
	if err != nil {
		return ""
	}
	tok, err := cred.GetToken(ctx, armTokenOptions())
	if err != nil {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://management.azure.com/subscriptions/"+subID+"?api-version=2022-12-01", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+tok.Token)
	resp, err := doWithRetry(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return ""
	}
	var body struct {
		TenantID string `json:"tenantId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return ""
	}
	return body.TenantID
}
