package azure

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/subscription/armsubscription"

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

// listSubscriptionsSDK pulls the caller's subscriptions from the ARM
// subscriptions client. Same shape as the old `az account list` output
// so callers can use the result interchangeably.
func (a *Azure) listSubscriptionsSDK(ctx context.Context) ([]provider.Node, error) {
	cred, err := defaultCredential()
	if err != nil {
		return nil, err
	}
	client, err := armsubscription.NewSubscriptionsClient(cred, nil)
	if err != nil {
		return nil, err
	}

	pager := client.NewListPager(nil)
	var subs []provider.Node
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list subscriptions: %w", err)
		}
		for _, s := range page.Value {
			if s == nil || s.SubscriptionID == nil {
				continue
			}
			name := ""
			if s.DisplayName != nil {
				name = *s.DisplayName
			}
			state := ""
			if s.State != nil {
				state = string(*s.State)
			}
			// TenantID isn't on the Subscription model in this SDK
			// version — the tenants pager / fetchTenants() resolves it.
			subs = append(subs, provider.Node{
				ID:    *s.SubscriptionID,
				Name:  name,
				Kind:  provider.KindSubscription,
				State: state,
				Meta:  map[string]string{},
			})
		}
	}
	return subs, nil
}
