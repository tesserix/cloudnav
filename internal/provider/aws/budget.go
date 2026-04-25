package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tesserix/cloudnav/internal/provider"
)

// BillingSummary returns the account-wide forecast and monthly budget for
// the billing overlay header. Both lookups are best-effort — a caller
// without ce:GetCostForecast or budgets:DescribeBudgets permission just
// gets a BillingScope with those fields zeroed, never an error. We only
// return a real error when the underlying CLI invocation itself fails in a
// way that suggests the user needs to know (e.g. missing AWS CLI).
//
// AWS budgets and forecasts are set at the account (or organization) level
// rather than per service, which is why this surfaces as a separate
// summary rather than being folded into each per-service CostLine.
func (a *AWS) BillingSummary(ctx context.Context) (provider.BillingScope, error) {
	out := provider.BillingScope{}
	// Currency — Cost Explorer reports in the account's default currency
	// which we can't resolve without a round trip. Default to USD but let
	// forecast and budget calls override below.
	out.Currency = defaultCurrency

	if forecast, currency, ok := a.fetchCostForecast(ctx); ok {
		out.Forecast = forecast
		if currency != "" {
			out.Currency = currency
		}
	}
	if budget, currency, note, ok := a.fetchAccountBudget(ctx); ok {
		out.Budget = budget
		if currency != "" {
			out.Currency = currency
		}
		if note != "" {
			out.Note = note
		}
	}
	return out, nil
}

// fetchCostForecast queries ce:GetCostForecast for the projected UNBLENDED
// spend from today to end of month. Quiet about missing permissions — the
// UI renders em-dash when forecast is zero.
func (a *AWS) fetchCostForecast(ctx context.Context) (float64, string, bool) {
	now := time.Now().UTC()
	end := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	if !now.Before(end) {
		return 0, "", false
	}
	// SDK fast path — Cost Explorer GetCostForecast.
	if v, cur, sdkUsable, err := a.fetchForecastSDK(ctx, now, end); sdkUsable && err == nil && v > 0 {
		return v, cur, true
	}
	out, err := a.aws.Run(ctx,
		"ce", "get-cost-forecast",
		"--time-period", fmt.Sprintf("Start=%s,End=%s", now.Format("2006-01-02"), end.Format("2006-01-02")),
		"--metric", "UNBLENDED_COST",
		"--granularity", "MONTHLY",
		"--output", "json",
	)
	if err != nil {
		return 0, "", false
	}
	return parseForecast(out)
}

func parseForecast(data []byte) (float64, string, bool) {
	var env struct {
		Total struct {
			Amount string `json:"Amount"`
			Unit   string `json:"Unit"`
		} `json:"Total"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return 0, "", false
	}
	if env.Total.Amount == "" {
		return 0, "", false
	}
	var v float64
	if _, err := fmt.Sscanf(env.Total.Amount, "%f", &v); err != nil {
		return 0, "", false
	}
	return v, env.Total.Unit, v > 0
}

// fetchAccountBudget sums the monthly budget ceilings set on the current
// account via AWS Budgets. Returns (budget, currency, note, ok). A caller
// without Budgets permission gets ok=false with no error surface.
func (a *AWS) fetchAccountBudget(ctx context.Context) (float64, string, string, bool) {
	accountID, err := a.callerAccountID(ctx)
	if err != nil || accountID == "" {
		return 0, "", "", false
	}
	// SDK fast path — Budgets DescribeBudgets paginated. Same
	// "largest monthly ceiling" pick as the CLI parser.
	if amt, cur, sdkUsable, err := a.fetchBudgetSDK(ctx, accountID); sdkUsable && err == nil && amt > 0 {
		return amt, cur, "", true
	}
	out, err := a.aws.Run(ctx,
		"budgets", "describe-budgets",
		"--account-id", accountID,
		"--output", "json",
	)
	if err != nil {
		// Accounts without any budgets respond with an empty list *or*
		// 404. Neither should surface as an error — it means "no budget
		// set", which is a valid state. Only real API failures (403 from
		// a missing IAM permission) should silently fall through.
		return 0, "", "", false
	}
	return parseBudgets(out)
}

func parseBudgets(data []byte) (float64, string, string, bool) {
	var env struct {
		Budgets []struct {
			BudgetName  string `json:"BudgetName"`
			BudgetLimit struct {
				Amount string `json:"Amount"`
				Unit   string `json:"Unit"`
			} `json:"BudgetLimit"`
			TimeUnit string `json:"TimeUnit"`
		} `json:"Budgets"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return 0, "", "", false
	}
	var max float64
	var currency string
	var note string
	count := 0
	for _, b := range env.Budgets {
		// Monthly budgets only — annual / quarterly ceilings aren't
		// comparable to the MTD actual the TUI shows.
		if !strings.EqualFold(b.TimeUnit, "MONTHLY") {
			continue
		}
		var v float64
		if _, err := fmt.Sscanf(b.BudgetLimit.Amount, "%f", &v); err != nil {
			continue
		}
		if v > max {
			max = v
			currency = b.BudgetLimit.Unit
		}
		count++
	}
	if count > 1 {
		note = fmt.Sprintf("%d monthly budgets — showing largest", count)
	}
	return max, currency, note, max > 0
}

// callerAccountID returns the current caller's AWS account id, needed by
// the Budgets API. Cached in memory across BillingSummary() invocations
// when we start calling it more than once per overlay load.
func (a *AWS) callerAccountID(ctx context.Context) (string, error) {
	// SDK fast path — sts:GetCallerIdentity, no subprocess.
	if node, sdkUsable, err := a.callerIdentitySDK(ctx); sdkUsable && err == nil {
		if id, ok := node.Meta["accountId"]; ok && id != "" {
			return id, nil
		}
		return node.ID, nil
	}
	out, err := a.aws.Run(ctx, "sts", "get-caller-identity", "--output", "json")
	if err != nil {
		return "", err
	}
	var c callerJSON
	if err := json.Unmarshal(out, &c); err != nil {
		return "", err
	}
	return c.Account, nil
}

// Ensure AWS implements the optional summary interface at compile time.
var _ provider.BillingSummarer = (*AWS)(nil)
