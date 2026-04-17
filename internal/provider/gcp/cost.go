package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tesserix/cloudnav/internal/provider"
)

const billingTableEnv = "CLOUDNAV_GCP_BILLING_TABLE"

func (g *GCP) Costs(ctx context.Context, parent provider.Node) (map[string]string, error) {
	if parent.Kind != provider.KindCloud {
		return nil, fmt.Errorf("gcp: cost is per-project across accessible billing export")
	}
	table := os.Getenv(billingTableEnv)
	if table == "" {
		return nil, fmt.Errorf("gcp cost: set %s=project.dataset.table (requires BigQuery billing export; see cloud.google.com/billing/docs/how-to/export-data-bigquery)", billingTableEnv)
	}
	query := fmt.Sprintf(
		"SELECT project.id AS project_id, ROUND(SUM(cost), 2) AS total, currency "+
			"FROM `%s` "+
			"WHERE usage_start_time >= TIMESTAMP_TRUNC(CURRENT_TIMESTAMP(), MONTH) "+
			"GROUP BY project_id, currency",
		table,
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
			return nil, fmt.Errorf("gcp bq query: %w", err)
		}
	}
	return parseBQCost(out)
}

func parseBQCost(data []byte) (map[string]string, error) {
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
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		if r.ProjectID == "" {
			continue
		}
		out[strings.ToLower(r.ProjectID)] = formatCostGCP(r.Total, r.Currency)
	}
	return out, nil
}

func formatCostGCP(amount float64, currency string) string {
	var symbol string
	switch strings.ToUpper(currency) {
	case "USD", "":
		symbol = "$"
	case "GBP":
		symbol = "£"
	case "EUR":
		symbol = "€"
	case "INR":
		symbol = "₹"
	case "JPY":
		symbol = "¥"
	default:
		symbol = currency + " "
	}
	return fmt.Sprintf("%s%.2f", symbol, amount)
}
