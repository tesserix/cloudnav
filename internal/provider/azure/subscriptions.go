package azure

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/subscription/armsubscription"

	"github.com/tesserix/cloudnav/internal/provider"
)

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
