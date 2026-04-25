package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tesserix/cloudnav/internal/provider"
)

// Billing returns month-to-date + previous-month numeric costs per project
// via the BigQuery billing export. Implements provider.Billing so the TUI's
// `B` overlay renders a portfolio-style view with real MoM deltas.
//
// When the BQ export isn't configured we return a single CostLine whose
// Note explains the setup step — the overlay shows that instead of a raw
// error. The GCPBillingSetup() diagnostic drives a richer checklist view.
func (g *GCP) Billing(ctx context.Context) ([]provider.CostLine, error) {
	table := g.billingTableResolved()
	if table == "" {
		if detected := g.autoDetectBillingTable(ctx); detected != "" {
			g.billingTable = detected
			table = detected
		}
	}
	if table == "" {
		return []provider.CostLine{{
			Label: "(cost unavailable)",
			Note:  "Configure a BigQuery billing export — set CLOUDNAV_GCP_BILLING_TABLE or run the `B` setup flow.",
		}}, nil
	}

	now := time.Now().UTC()
	cur, err := g.costsRaw(ctx, table, firstOfMonth(now), now.AddDate(0, 0, 1))
	if err != nil {
		return nil, fmt.Errorf("gcp billing (current): %w", err)
	}
	fromLast, toLast := lastMonthSamePeriod(now)
	last, _ := g.costsRaw(ctx, table, fromLast, toLast.AddDate(0, 0, 1))

	out := make([]provider.CostLine, 0, len(cur))
	for proj, c := range cur {
		line := provider.CostLine{
			Label:    proj,
			Current:  c.amount,
			Currency: c.currency,
		}
		if l, ok := last[proj]; ok {
			line.LastMonth = l.amount
		}
		out = append(out, line)
	}
	return out, nil
}

// BillingSummary returns account-wide budget + linear forecast for the
// billing overlay header. Budget uses the Cloud Billing Budgets API
// (billingbudgets.googleapis.com); forecast is a straight-line projection
// from the MTD total across all projects — same shape Azure's forecast
// takes under the hood, but explicitly called out as an estimate since
// GCP doesn't expose a first-party forecast API on the BQ export.
func (g *GCP) BillingSummary(ctx context.Context) (provider.BillingScope, error) {
	scope := provider.BillingScope{Currency: "USD"}

	// 1. Fetch monthly budget ceiling across the primary billing account.
	acct := g.primaryBillingAccount(ctx)
	if acct != "" {
		if budget, currency, note := g.fetchBillingAccountBudget(ctx, acct); budget > 0 {
			scope.Budget = budget
			if currency != "" {
				scope.Currency = currency
			}
			if note != "" {
				scope.Note = note
			}
		}
	}

	// 2. Straight-line forecast from MTD.
	table := g.billingTableResolved()
	if table != "" {
		if projection := g.forecastLinear(ctx, table); projection > 0 {
			scope.Forecast = projection
		}
	}
	return scope, nil
}

// fetchBillingAccountBudget queries the Cloud Billing Budgets API
// for a billing account, sums the monthly ceiling(s), and returns
// the largest one along with a note about aggregation when there
// are multiple. Routes through the SDK first; falls back to gcloud
// when ADC isn't usable.
func (g *GCP) fetchBillingAccountBudget(ctx context.Context, acct string) (float64, string, string) {
	if amt, cur, note, sdkUsable, err := g.fetchBudgetsSDK(ctx, acct); sdkUsable && err == nil {
		return amt, cur, note
	}
	out, err := g.gcloud.Run(ctx,
		"billing", "budgets", "list",
		"--billing-account="+acct,
		"--format=json",
	)
	if err != nil {
		return 0, "", ""
	}
	return parseGCPBudgets(out)
}

func parseGCPBudgets(data []byte) (float64, string, string) {
	var budgets []struct {
		DisplayName string `json:"displayName"`
		Amount      struct {
			SpecifiedAmount struct {
				Units        string `json:"units"`
				CurrencyCode string `json:"currencyCode"`
			} `json:"specifiedAmount"`
		} `json:"amount"`
	}
	if err := json.Unmarshal(data, &budgets); err != nil {
		return 0, "", ""
	}
	var max float64
	var currency string
	for _, b := range budgets {
		var v float64
		if _, err := fmt.Sscanf(b.Amount.SpecifiedAmount.Units, "%f", &v); err != nil {
			continue
		}
		if v > max {
			max = v
			currency = b.Amount.SpecifiedAmount.CurrencyCode
		}
	}
	note := ""
	if len(budgets) > 1 {
		note = fmt.Sprintf("%d budgets — showing largest", len(budgets))
	}
	return max, currency, note
}

// forecastLinear projects month-end spend by extrapolating MTD pro-rata
// across the full month. Conservative — doesn't account for periodic
// workload patterns — but for most steady-state workloads it's within 10%
// and gives users a useful "am I going to blow through budget?" signal.
func (g *GCP) forecastLinear(ctx context.Context, table string) float64 {
	now := time.Now().UTC()
	cur, err := g.costsRaw(ctx, table, firstOfMonth(now), now.AddDate(0, 0, 1))
	if err != nil {
		return 0
	}
	var total float64
	for _, c := range cur {
		total += c.amount
	}
	if total <= 0 {
		return 0
	}
	// Days elapsed (including today) and days in the month.
	daysElapsed := now.Day()
	if daysElapsed < 1 {
		daysElapsed = 1
	}
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	nextMonth := first.AddDate(0, 1, 0)
	daysInMonth := int(nextMonth.Sub(first).Hours() / 24)
	if daysInMonth < 1 {
		daysInMonth = 30
	}
	return total * float64(daysInMonth) / float64(daysElapsed)
}

// costsRaw runs the BQ billing export query for a window and returns
// per-project numeric costs. Shared between Billing() and the linear
// forecast so both see the same currency and numbers.
func (g *GCP) costsRaw(ctx context.Context, table string, from, to time.Time) (map[string]costRow, error) {
	query := fmt.Sprintf(
		"SELECT project.id AS project_id, ROUND(SUM(cost), 2) AS total, currency "+
			"FROM `%s` "+
			"WHERE usage_start_time >= TIMESTAMP('%s') "+
			"  AND usage_start_time < TIMESTAMP('%s') "+
			"GROUP BY project_id, currency",
		table,
		from.Format("2006-01-02T15:04:05Z"),
		to.Format("2006-01-02T15:04:05Z"),
	)
	out, err := g.gcloud.Run(ctx,
		"alpha", "bq", "query",
		"--nouse_legacy_sql",
		"--format=json",
		query,
	)
	if err != nil {
		out, err = g.gcloud.Run(ctx,
			"bq", "query",
			"--nouse_legacy_sql",
			"--format=json",
			query,
		)
		if err != nil {
			return nil, err
		}
	}
	return parseBQCostRows(out)
}

type costRow struct {
	amount   float64
	currency string
}

func parseBQCostRows(data []byte) (map[string]costRow, error) {
	var rows []struct {
		ProjectID string  `json:"project_id"`
		Total     float64 `json:"total"`
		Currency  string  `json:"currency"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		var raw []map[string]any
		if err2 := json.Unmarshal(data, &raw); err2 != nil {
			return nil, fmt.Errorf("parse bq json: %w", err)
		}
		rows = rows[:0]
		for _, r := range raw {
			pid, _ := r["project_id"].(string)
			cur, _ := r["currency"].(string)
			var total float64
			switch v := r["total"].(type) {
			case float64:
				total = v
			case string:
				_, _ = fmt.Sscanf(v, "%f", &total)
			}
			rows = append(rows, struct {
				ProjectID string  `json:"project_id"`
				Total     float64 `json:"total"`
				Currency  string  `json:"currency"`
			}{ProjectID: pid, Total: total, Currency: cur})
		}
	}
	out := make(map[string]costRow, len(rows))
	for _, r := range rows {
		if r.ProjectID == "" {
			continue
		}
		out[strings.ToLower(r.ProjectID)] = costRow{amount: r.Total, currency: r.Currency}
	}
	return out, nil
}

// firstOfMonth mirrors the AWS helper so both providers share the same
// window semantics when the TUI merges data.
func firstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func lastMonthSamePeriod(now time.Time) (time.Time, time.Time) {
	now = now.UTC()
	firstThis := firstOfMonth(now)
	from := firstThis.AddDate(0, -1, 0)
	to := from.AddDate(0, 0, now.Day()-1)
	lastDay := firstThis.AddDate(0, 0, -1)
	if to.After(lastDay) {
		to = lastDay
	}
	if to.Before(from) {
		to = from
	}
	return from, to
}

// Ensure GCP implements Billing and the optional summary at compile time.
var (
	_ provider.Billing         = (*GCP)(nil)
	_ provider.BillingSummarer = (*GCP)(nil)
)
