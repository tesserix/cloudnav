package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/tesserix/cloudnav/internal/provider"
)

// subIDs returns every subscription id the caller can see. Used by
// billing / cost-history / pim tenant enumeration — the places that
// just need the ids, not the full Node objects. Tries the SDK path
// first and falls back to `az account list` when the credential chain
// can't resolve.
func (a *Azure) subIDs(ctx context.Context) ([]string, error) {
	subs, err := a.listSubscriptionsSDK(ctx)
	if err != nil {
		out, cliErr := a.az.Run(ctx, "account", "list", "-o", "json")
		if cliErr != nil {
			return nil, cliErr
		}
		cliSubs, parseErr := parseSubs(out)
		if parseErr != nil {
			return nil, parseErr
		}
		subs = cliSubs
	}
	ids := make([]string, 0, len(subs))
	for _, s := range subs {
		if s.ID != "" {
			ids = append(ids, s.ID)
		}
	}
	return ids, nil
}

// listSubscriptionsSDK pulls the caller's subscriptions via the ARM
// REST endpoint using an SDK-minted token. We hit the REST URL directly
// (rather than armsubscription.NewSubscriptionsClient.NewListPager)
// because the SDK model strips tenantId from the response, which breaks
// the TENANT column. Same shape as the old `az account list` output so
// callers can use the result interchangeably.
func (a *Azure) listSubscriptionsSDK(ctx context.Context) ([]provider.Node, error) {
	cred, err := defaultCredential()
	if err != nil {
		return nil, err
	}
	tok, err := cred.GetToken(ctx, armTokenOptions())
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://management.azure.com/subscriptions?api-version=2022-12-01", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok.Token)
	resp, err := doWithRetry(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("list subscriptions: HTTP %d", resp.StatusCode)
	}
	var env struct {
		Value []struct {
			SubscriptionID string `json:"subscriptionId"`
			DisplayName    string `json:"displayName"`
			State          string `json:"state"`
			TenantID       string `json:"tenantId"`
		} `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("parse subscriptions: %w", err)
	}
	subs := make([]provider.Node, 0, len(env.Value))
	for _, s := range env.Value {
		if s.SubscriptionID == "" {
			continue
		}
		meta := map[string]string{}
		if s.TenantID != "" {
			meta["tenantId"] = s.TenantID
		}
		subs = append(subs, provider.Node{
			ID:    s.SubscriptionID,
			Name:  s.DisplayName,
			Kind:  provider.KindSubscription,
			State: s.State,
			Meta:  meta,
		})
	}
	return subs, nil
}

