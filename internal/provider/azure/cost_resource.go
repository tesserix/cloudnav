package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (a *Azure) ResourceCosts(ctx context.Context, subID, rgName string) (map[string]string, error) {
	current, err := a.queryResourceCosts(ctx, subID, rgName, nil, nil)
	if err != nil {
		return nil, err
	}
	from, to := lastMonthSamePeriod(time.Now())
	last, _ := a.queryResourceCosts(ctx, subID, rgName, &from, &to)

	out := make(map[string]string, len(current))
	for id, cur := range current {
		if last == nil {
			out[id] = formatCost(cur.amount, cur.currency)
			continue
		}
		lc, ok := last[id]
		if !ok {
			out[id] = formatCost(cur.amount, cur.currency) + " new"
			continue
		}
		out[id] = formatCostWithDelta(cur, lc)
	}
	return out, nil
}

func (a *Azure) queryResourceCosts(ctx context.Context, subID, rg string, from, to *time.Time) (map[string]costCell, error) {
	body := map[string]any{
		"type":      "ActualCost",
		"timeframe": "MonthToDate",
		"dataset": map[string]any{
			"granularity": "None",
			"aggregation": map[string]any{
				"totalCost": map[string]any{"name": "PreTaxCost", "function": "Sum"},
			},
			"grouping": []any{
				map[string]any{"type": "Dimension", "name": "ResourceId"},
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
		"https://management.azure.com/subscriptions/%s/resourceGroups/%s/providers/Microsoft.CostManagement/query?api-version=2023-11-01",
		subID, rg,
	)
	out, err := a.postJSONForSub(ctx, subID, url, raw)
	if err != nil {
		return nil, err
	}
	return parseResourceCostCells(out)
}

func parseResourceCostCells(data []byte) (map[string]costCell, error) {
	var env struct {
		Properties struct {
			Columns []struct {
				Name string `json:"name"`
			} `json:"columns"`
			Rows [][]any `json:"rows"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse resource cost: %w", err)
	}
	costCol, idCol, currencyCol := -1, -1, -1
	for i, c := range env.Properties.Columns {
		switch c.Name {
		case colPreTaxCost, colCost:
			costCol = i
		case "ResourceId":
			idCol = i
		case colCurrency:
			currencyCol = i
		}
	}
	if costCol < 0 || idCol < 0 {
		return nil, fmt.Errorf("resource cost response missing expected columns")
	}
	out := make(map[string]costCell, len(env.Properties.Rows))
	for _, r := range env.Properties.Rows {
		if len(r) <= costCol || len(r) <= idCol {
			continue
		}
		amount, _ := r[costCol].(float64)
		id, _ := r[idCol].(string)
		if id == "" {
			continue
		}
		cell := costCell{amount: amount, currency: defaultCurrency}
		if currencyCol >= 0 && len(r) > currencyCol {
			if c, ok := r[currencyCol].(string); ok {
				cell.currency = c
			}
		}
		out[strings.ToLower(id)] = cell
	}
	return out, nil
}
