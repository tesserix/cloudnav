package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/tesserix/cloudnav/internal/provider"
)

type ServiceCost struct {
	Service   string
	Current   float64
	LastMonth float64
	Currency  string
}

func (a *AWS) ServiceCosts(ctx context.Context) ([]ServiceCost, error) {
	now := time.Now().UTC()
	current, err := a.fetchCostGroupedBy(ctx, firstOfMonth(now), now.AddDate(0, 0, 1), "SERVICE")
	if err != nil {
		return nil, err
	}
	fromLast, toLast := lastMonthSamePeriod(now)
	last, _ := a.fetchCostGroupedBy(ctx, fromLast, toLast.AddDate(0, 0, 1), "SERVICE")

	out := make([]ServiceCost, 0, len(current))
	for svc, cur := range current {
		sc := ServiceCost{Service: svc, Current: cur.amount, Currency: cur.currency}
		if last != nil {
			if lc, ok := last[svc]; ok {
				sc.LastMonth = lc.amount
			}
		}
		out = append(out, sc)
	}
	return out, nil
}

// Billing returns AWS cost per service (this month + last) so the TUI's `B`
// overlay can render a portfolio-style breakdown with MoM deltas. Implements
// provider.Billing.
func (a *AWS) Billing(ctx context.Context) ([]provider.CostLine, error) {
	services, err := a.ServiceCosts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]provider.CostLine, 0, len(services))
	for _, s := range services {
		out = append(out, provider.CostLine{
			Label:     s.Service,
			Current:   s.Current,
			LastMonth: s.LastMonth,
			Currency:  s.Currency,
		})
	}
	return out, nil
}

func (a *AWS) fetchCostGroupedBy(ctx context.Context, from, to time.Time, dimension string) (map[string]costSample, error) {
	// SDK fast path — Cost Explorer with arbitrary group-by
	// dimension (REGION / SERVICE / LINKED_ACCOUNT / etc.).
	if res, sdkUsable, err := a.fetchCostSDK(ctx, from, to, dimension); sdkUsable && err == nil {
		return res, nil
	}
	out, err := a.aws.Run(ctx,
		"ce", "get-cost-and-usage",
		"--time-period", fmt.Sprintf("Start=%s,End=%s", from.Format("2006-01-02"), to.Format("2006-01-02")),
		"--granularity", "MONTHLY",
		"--metrics", "UnblendedCost",
		"--group-by", fmt.Sprintf("Type=DIMENSION,Key=%s", dimension),
		"--output", "json",
	)
	if err != nil {
		return nil, err
	}
	return parseCostUsage(out)
}
