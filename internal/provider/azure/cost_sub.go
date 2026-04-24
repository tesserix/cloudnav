package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type SubscriptionCost struct {
	SubscriptionID   string
	SubscriptionName string
	Current          float64
	LastMonth        float64
	Currency         string
	Error            string
	// Forecast is the projected *total* month-end spend (current MTD plus
	// the provider's estimate of the remainder). Zero means no forecast
	// was available — the TUI renders "—" rather than "$0".
	Forecast float64
	// Budget is the monthly ceiling configured for this subscription (via
	// Microsoft.Consumption/budgets). Zero means no budget is set.
	Budget float64
}

func (a *Azure) SubscriptionCosts(ctx context.Context, subIDs []string) ([]SubscriptionCost, error) {
	if len(subIDs) == 0 {
		return nil, nil
	}
	if err := a.ensureSubsCache(ctx); err != nil {
		return nil, err
	}
	fromLast, toLast := lastMonthSamePeriod(time.Now())

	type result struct {
		cost SubscriptionCost
	}
	results := make(chan result, len(subIDs))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, id := range subIDs {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res := SubscriptionCost{SubscriptionID: id, SubscriptionName: a.subName(id)}
			cur, err := a.querySubTotal(ctx, id, nil, nil)
			if err != nil {
				res.Error = trimErr(err.Error())
				results <- result{cost: res}
				return
			}
			res.Current = cur.amount
			res.Currency = cur.currency

			// Last-month / forecast / budget are independent and additive —
			// fan them out in parallel inside the current semaphore slot so
			// users don't wait 3× sequentially. Local vars avoid concurrent
			// writes to res.
			var (
				lastAmt, fcstRem, budget float64
				inner                    sync.WaitGroup
			)
			inner.Add(3)
			go func() {
				defer inner.Done()
				if last, err := a.querySubTotal(ctx, id, &fromLast, &toLast); err == nil {
					lastAmt = last.amount
				}
			}()
			go func() {
				defer inner.Done()
				if r, err := a.querySubForecast(ctx, id); err == nil && r > 0 {
					fcstRem = r
				}
			}()
			go func() {
				defer inner.Done()
				if b, err := a.querySubBudget(ctx, id); err == nil && b > 0 {
					budget = b
				}
			}()
			inner.Wait()
			res.LastMonth = lastAmt
			if fcstRem > 0 {
				res.Forecast = res.Current + fcstRem
			}
			res.Budget = budget
			results <- result{cost: res}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	out := make([]SubscriptionCost, 0, len(subIDs))
	for r := range results {
		out = append(out, r.cost)
	}
	return out, nil
}

func trimErr(s string) string {
	for _, kw := range []string{"AuthorizationFailed", "Forbidden", "does not have authorization"} {
		if strings.Contains(s, kw) {
			return "no cost read access"
		}
	}
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}

func (a *Azure) ensureSubsCache(ctx context.Context) error {
	a.mu.RLock()
	has := a.subs != nil
	a.mu.RUnlock()
	if has {
		return nil
	}
	_, err := a.Root(ctx)
	return err
}

func (a *Azure) querySubTotal(ctx context.Context, subID string, from, to *time.Time) (costCell, error) {
	body := map[string]any{
		"type":      "ActualCost",
		"timeframe": "MonthToDate",
		"dataset": map[string]any{
			"granularity": "None",
			"aggregation": map[string]any{
				"totalCost": map[string]any{"name": "PreTaxCost", "function": "Sum"},
			},
		},
	}
	if from != nil && to != nil {
		body["timeframe"] = timeframeCustom
		body["timePeriod"] = map[string]any{
			"from": from.UTC().Format("2006-01-02T15:04:05Z"),
			"to":   to.UTC().Format("2006-01-02T15:04:05Z"),
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return costCell{}, err
	}
	url := fmt.Sprintf(
		"https://management.azure.com/subscriptions/%s/providers/Microsoft.CostManagement/query?api-version=2023-11-01",
		subID,
	)
	out, err := a.postJSONForSub(ctx, subID, url, raw)
	if err != nil {
		return costCell{}, err
	}
	return parseSubTotal(out)
}

func parseSubTotal(data []byte) (costCell, error) {
	var env struct {
		Properties struct {
			Columns []struct {
				Name string `json:"name"`
			} `json:"columns"`
			Rows [][]any `json:"rows"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return costCell{}, fmt.Errorf("parse sub cost: %w", err)
	}
	costCol, currencyCol := -1, -1
	for i, c := range env.Properties.Columns {
		switch c.Name {
		case colPreTaxCost, colCost:
			costCol = i
		case colCurrency:
			currencyCol = i
		}
	}
	if costCol < 0 || len(env.Properties.Rows) == 0 {
		return costCell{}, nil
	}
	row := env.Properties.Rows[0]
	amount, _ := row[costCol].(float64)
	cell := costCell{amount: amount, currency: defaultCurrency}
	if currencyCol >= 0 && len(row) > currencyCol {
		if c, ok := row[currencyCol].(string); ok {
			cell.currency = c
		}
	}
	return cell, nil
}
