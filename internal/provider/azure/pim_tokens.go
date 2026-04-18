package azure

import (
	"context"
	"encoding/json"
	"fmt"
)

// tenantToken returns a bearer token for the Azure Management API audience.
func (a *Azure) tenantToken(ctx context.Context, tenantID string) (string, error) {
	return a.tenantTokenFor(ctx, tenantID, armResource)
}

// tenantTokenFor returns a bearer token for a specific audience — ARM for
// subscription-scoped calls, Graph for Entra / Groups PIM. Delegates to `az
// account get-access-token`, which transparently refreshes cached tokens, so
// callers get a headless experience as long as `az login` covers the tenant.
func (a *Azure) tenantTokenFor(ctx context.Context, tenantID, resource string) (string, error) {
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
	}
	if err := json.Unmarshal(out, &t); err != nil {
		return "", err
	}
	if t.AccessToken == "" {
		return "", fmt.Errorf("empty token for tenant %s (audience %s)", tenantID, resource)
	}
	return t.AccessToken, nil
}
