package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/tesserix/cloudnav/internal/provider"
)

const (
	colPreTaxCost   = "PreTaxCost"
	colCost         = "Cost"
	colCurrency     = "Currency"
	timeframeCustom = "Custom"
)

type costCell struct {
	amount   float64
	currency string
}

func (a *Azure) Costs(ctx context.Context, parent provider.Node) (map[string]string, error) {
	if parent.Kind != provider.KindSubscription {
		return nil, fmt.Errorf("azure: cost breakdown is supported on subscription scope, got %q", parent.Kind)
	}
	subID := parent.ID
	current, err := a.queryRGCosts(ctx, subID, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("azure cost query: %w", err)
	}
	from, to := lastMonthSamePeriod(time.Now())
	last, lastErr := a.queryRGCosts(ctx, subID, &from, &to)

	out := make(map[string]string, len(current))
	for rg, cur := range current {
		if lastErr != nil || last == nil {
			out[rg] = formatCost(cur.amount, cur.currency)
			continue
		}
		lc, ok := last[rg]
		if !ok {
			out[rg] = fmt.Sprintf("%s  new", formatCost(cur.amount, cur.currency))
			continue
		}
		out[rg] = formatCostWithDelta(cur, lc)
	}
	return out, nil
}

func (a *Azure) queryRGCosts(ctx context.Context, subID string, from, to *time.Time) (map[string]costCell, error) {
	body := map[string]any{
		"type":      "ActualCost",
		"timeframe": "MonthToDate",
		"dataset": map[string]any{
			"granularity": "None",
			"aggregation": map[string]any{
				"totalCost": map[string]any{"name": "PreTaxCost", "function": "Sum"},
			},
			"grouping": []any{
				map[string]any{"type": "Dimension", "name": "ResourceGroupName"},
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
		return nil, err
	}
	url := fmt.Sprintf(
		"https://management.azure.com/subscriptions/%s/providers/Microsoft.CostManagement/query?api-version=2023-11-01",
		subID,
	)
	out, err := a.az.Run(ctx, "rest", "--method", "POST", "--url", url, "--body", string(raw))
	if err != nil {
		return nil, err
	}
	return parseCostCells(out)
}

func parseCostCells(data []byte) (map[string]costCell, error) {
	var envelope struct {
		Properties struct {
			Columns []struct {
				Name string `json:"name"`
			} `json:"columns"`
			Rows [][]any `json:"rows"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("parse cost response: %w", err)
	}
	costCol, rgCol, currencyCol := -1, -1, -1
	for i, c := range envelope.Properties.Columns {
		switch c.Name {
		case colPreTaxCost, colCost:
			costCol = i
		case "ResourceGroupName", "ResourceGroup":
			rgCol = i
		case colCurrency:
			currencyCol = i
		}
	}
	if costCol < 0 || rgCol < 0 {
		return nil, fmt.Errorf("cost response missing expected columns")
	}
	out := make(map[string]costCell, len(envelope.Properties.Rows))
	for _, r := range envelope.Properties.Rows {
		if len(r) <= costCol || len(r) <= rgCol {
			continue
		}
		amount, ok := r[costCol].(float64)
		if !ok {
			continue
		}
		rg, ok := r[rgCol].(string)
		if !ok || rg == "" {
			continue
		}
		currency := "USD"
		if currencyCol >= 0 && len(r) > currencyCol {
			if c, ok := r[currencyCol].(string); ok {
				currency = c
			}
		}
		out[strings.ToLower(rg)] = costCell{amount: amount, currency: currency}
	}
	return out, nil
}

func parseCosts(data []byte) (map[string]string, error) {
	cells, err := parseCostCells(data)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(cells))
	for rg, c := range cells {
		out[rg] = formatCost(c.amount, c.currency)
	}
	return out, nil
}

func formatCost(amount float64, currency string) string {
	return fmt.Sprintf("%s%.2f", currencySymbol(currency), amount)
}

func formatCostWithDelta(current, last costCell) string {
	base := formatCost(current.amount, current.currency)
	if last.amount == 0 {
		if current.amount == 0 {
			return base
		}
		return base + " \x1b[1;31mnew\x1b[0m"
	}
	delta := (current.amount - last.amount) / last.amount * 100
	switch {
	case delta > 2:
		return fmt.Sprintf("%s \x1b[1;31m↑%d%%\x1b[0m", base, int(math.Round(delta)))
	case delta < -2:
		return fmt.Sprintf("%s \x1b[1;32m↓%d%%\x1b[0m", base, int(math.Round(-delta)))
	default:
		return base + " \x1b[2;37m→\x1b[0m"
	}
}

func lastMonthSamePeriod(now time.Time) (time.Time, time.Time) {
	now = now.UTC()
	firstThis := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	from := firstThis.AddDate(0, -1, 0)
	to := from.AddDate(0, 0, now.Day()-1)
	lastDayLastMonth := firstThis.AddDate(0, 0, -1)
	if to.After(lastDayLastMonth) {
		to = lastDayLastMonth
	}
	if to.Before(from) {
		to = from
	}
	return from, to
}

func currencySymbol(code string) string {
	switch strings.ToUpper(code) {
	case "USD":
		return "$"
	case "GBP":
		return "£"
	case "EUR":
		return "€"
	case "INR":
		return "₹"
	case "JPY":
		return "¥"
	case "AUD":
		return "A$"
	case "CAD":
		return "C$"
	default:
		return code + " "
	}
}
