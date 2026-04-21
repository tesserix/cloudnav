package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// querySubForecast returns the projected *incremental* spend from today
// through the end of the current billing month for a single subscription.
// The caller adds this to the MTD-actual to get a "projected month total"
// figure. Zero (with nil error) is a valid response — on the first of the
// month the incremental window is one day and may produce no data.
//
// The Cost Management /forecast endpoint computes the projection from
// historical usage; it will error with 400 on subs that have no usage
// history or no Cost Management Reader role. Those are non-fatal: the
// caller simply leaves Forecast=0 for that sub.
func (a *Azure) querySubForecast(ctx context.Context, subID string) (float64, error) {
	now := time.Now().UTC()
	year, month, _ := now.Date()
	// Last day of the current month at end-of-day.
	endOfMonth := time.Date(year, month+1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Second)
	// Start from the beginning of today so daily forecast rows include today.
	startOfToday := time.Date(year, month, now.Day(), 0, 0, 0, 0, time.UTC)
	if !startOfToday.Before(endOfMonth) {
		// We're already past the last day of the month — nothing to forecast.
		return 0, nil
	}

	body := map[string]any{
		"type":      "Usage",
		"timeframe": timeframeCustom,
		"timePeriod": map[string]any{
			"from": startOfToday.Format("2006-01-02T15:04:05Z"),
			"to":   endOfMonth.Format("2006-01-02T15:04:05Z"),
		},
		"dataset": map[string]any{
			"granularity": "Daily",
			"aggregation": map[string]any{
				"totalCost": map[string]any{"name": "PreTaxCost", "function": "Sum"},
			},
		},
		"includeActualCost":       false,
		"includeFreshPartialCost": false,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	url := fmt.Sprintf(
		"https://management.azure.com/subscriptions/%s/providers/Microsoft.CostManagement/forecast?api-version=2023-11-01",
		subID,
	)
	out, err := a.postJSONForSub(ctx, subID, url, raw)
	if err != nil {
		return 0, err
	}
	return parseForecastTotal(out)
}

// parseForecastTotal sums the daily forecast rows into a single projected
// incremental total. Cost column handling mirrors parseSubTotal so the two
// endpoints stay consistent when Azure renames columns between API
// versions.
func parseForecastTotal(data []byte) (float64, error) {
	var env struct {
		Properties struct {
			Columns []struct {
				Name string `json:"name"`
			} `json:"columns"`
			Rows [][]any `json:"rows"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return 0, fmt.Errorf("parse forecast: %w", err)
	}
	costCol := -1
	for i, c := range env.Properties.Columns {
		switch c.Name {
		case colPreTaxCost, colCost:
			costCol = i
		}
	}
	if costCol < 0 {
		return 0, nil
	}
	var total float64
	for _, row := range env.Properties.Rows {
		if len(row) <= costCol {
			continue
		}
		if v, ok := row[costCol].(float64); ok {
			total += v
		}
	}
	return total, nil
}
