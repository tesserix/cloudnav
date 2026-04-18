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

// SetBillingTable lets a caller (typically the TUI on startup) override the
// BQ billing-export table after construction. The env var still wins so
// CI/scripts keep working without rewriting config.
func (g *GCP) SetBillingTable(table string) {
	g.billingTable = table
}

// billingTableResolved picks the first non-empty source: explicit override
// from config, then the CLOUDNAV_GCP_BILLING_TABLE env var.
func (g *GCP) billingTableResolved() string {
	if v := os.Getenv(billingTableEnv); v != "" {
		return v
	}
	return g.billingTable
}

func (g *GCP) Costs(ctx context.Context, parent provider.Node) (map[string]string, error) {
	if parent.Kind != provider.KindCloud {
		return nil, fmt.Errorf("gcp: cost is per-project across accessible billing export")
	}
	table := g.billingTableResolved()
	if table == "" {
		// Try auto-detection once before giving up.
		if detected, acct := g.autoDetectBillingTable(ctx); detected != "" {
			g.billingTable = detected
			table = detected
			_ = acct
		}
	}
	if table == "" {
		// Link directly to the billing export setup page when we know the
		// billing account for the current default project.
		setupURL := "https://console.cloud.google.com/billing/_/export"
		if acct := g.primaryBillingAccount(ctx); acct != "" {
			setupURL = fmt.Sprintf("https://console.cloud.google.com/billing/%s/export", acct)
		}
		return nil, fmt.Errorf("GCP per-project cost needs BigQuery billing export (Google doesn't expose a cost API without it).\n\n  1. enable it here: %s\n  2. wait a few hours for the first export to land\n  3. tell cloudnav where the table lives, once, with either:\n       export %s=<project>.<dataset>.<table>\n       or add {\"gcp\":{\"billing_table\":\"<project>.<dataset>.<table>\"}} to ~/.config/cloudnav/config.json",
			setupURL, billingTableEnv)
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

// autoDetectBillingTable walks a tiny discovery chain: default project →
// its billing account → datasets matching "billing" → the canonical
// gcp_billing_export_v1_* table. Returns "" when anything on that chain
// isn't set up. Best-effort and cached per-process.
func (g *GCP) autoDetectBillingTable(ctx context.Context) (string, string) {
	projectOut, err := g.gcloud.Run(ctx, "config", "get-value", "project", "--format=value(core.project)")
	if err != nil {
		return "", ""
	}
	project := strings.TrimSpace(string(projectOut))
	if project == "" {
		return "", ""
	}
	acctOut, err := g.gcloud.Run(ctx, "billing", "projects", "describe", project, "--format=value(billingAccountName)")
	if err != nil {
		return "", ""
	}
	acct := strings.TrimPrefix(strings.TrimSpace(string(acctOut)), "billingAccounts/")
	if acct == "" {
		return "", ""
	}
	// Billing export tables are named gcp_billing_export_v1_<ACCT> with dashes
	// replaced by underscores. Try a lightweight `bq show` on the conventional
	// dataset 'billing_export'; on success we know the table exists.
	table := fmt.Sprintf("%s.billing_export.gcp_billing_export_v1_%s", project, strings.ReplaceAll(acct, "-", "_"))
	if _, err := g.gcloud.Run(ctx, "alpha", "bq", "tables", "describe",
		"--dataset=billing_export",
		"--project="+project,
		"gcp_billing_export_v1_"+strings.ReplaceAll(acct, "-", "_"),
		"--format=value(tableReference.tableId)",
	); err == nil {
		return table, acct
	}
	return "", acct
}

// primaryBillingAccount returns the billing account for the gcloud default
// project. Used to deep-link into the Billing → Export setup page.
func (g *GCP) primaryBillingAccount(ctx context.Context) string {
	projectOut, err := g.gcloud.Run(ctx, "config", "get-value", "project", "--format=value(core.project)")
	if err != nil {
		return ""
	}
	project := strings.TrimSpace(string(projectOut))
	if project == "" {
		return ""
	}
	out, err := g.gcloud.Run(ctx, "billing", "projects", "describe", project, "--format=value(billingAccountName)")
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(string(out)), "billingAccounts/")
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
