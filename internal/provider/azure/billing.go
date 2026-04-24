package azure

import (
	"context"
	"fmt"

	"github.com/tesserix/cloudnav/internal/provider"
)

// Billing returns the current + last-month spend for every subscription
// the caller has access to, rolled up across all tenants. Implements
// provider.Billing so the TUI's `B` overlay can show it.
func (a *Azure) Billing(ctx context.Context) ([]provider.CostLine, error) {
	ids, err := a.subIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("azure billing: %w", err)
	}
	rows, err := a.SubscriptionCosts(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := make([]provider.CostLine, 0, len(rows))
	for _, r := range rows {
		label := r.SubscriptionName
		if label == "" {
			label = r.SubscriptionID
		}
		note := r.Error
		result = append(result, provider.CostLine{
			Label:     label,
			Current:   r.Current,
			LastMonth: r.LastMonth,
			Currency:  r.Currency,
			Note:      note,
			Forecast:  r.Forecast,
			Budget:    r.Budget,
		})
	}
	return result, nil
}
