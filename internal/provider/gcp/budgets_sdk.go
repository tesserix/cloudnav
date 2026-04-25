package gcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	budgets "cloud.google.com/go/billing/budgets/apiv1"
	budgetspb "cloud.google.com/go/billing/budgets/apiv1/budgetspb"
	"google.golang.org/api/iterator"
)

// Cloud Billing Budgets SDK lifecycle.
var (
	budgetsOnce    sync.Once
	budgetsClient  *budgets.BudgetClient
	budgetsInitErr error
)

func (g *GCP) budgetsSDKClient(ctx context.Context) (*budgets.BudgetClient, error) {
	budgetsOnce.Do(func() {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		client, err := budgets.NewBudgetClient(c)
		if err != nil {
			budgetsInitErr = err
			return
		}
		budgetsClient = client
	})
	return budgetsClient, budgetsInitErr
}

// fetchBudgetsSDK iterates every budget on a billing account, picks
// the largest monthly ceiling (matches the gcloud-CLI parser
// behaviour), and returns the canonical (amount, currency, note)
// triple.
//
// Returns (0, "", "", false, err) when the SDK isn't usable so the
// caller falls back to `gcloud billing budgets list`.
func (g *GCP) fetchBudgetsSDK(ctx context.Context, acct string) (float64, string, string, bool, error) {
	client, err := g.budgetsSDKClient(ctx)
	if err != nil || client == nil {
		return 0, "", "", false, err
	}
	parent := "billingAccounts/" + acct
	it := client.ListBudgets(ctx, &budgetspb.ListBudgetsRequest{
		Parent: parent,
	})
	var (
		maxAmount float64
		currency  string
		count     int
	)
	for {
		b, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return 0, "", "", true, err
		}
		count++
		spec := b.GetAmount().GetSpecifiedAmount()
		if spec == nil {
			continue
		}
		// Money is { currency_code, units, nanos }. We coerce to
		// float64 the same way the CLI parser does — units +
		// nanos/1e9 — so the budget number matches whatever the
		// user sees in the cloud console.
		v := float64(spec.Units) + float64(spec.Nanos)/1e9
		if v > maxAmount {
			maxAmount = v
			currency = spec.CurrencyCode
		}
	}
	note := ""
	if count > 1 {
		note = fmt.Sprintf("%d budgets — showing largest", count)
	}
	return maxAmount, currency, note, true, nil
}

func closeBudgetsClient() error {
	if budgetsClient != nil {
		return budgetsClient.Close()
	}
	return nil
}
