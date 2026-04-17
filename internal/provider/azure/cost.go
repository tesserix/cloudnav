package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ResourceGroupCosts returns a map of lowercased resource-group name → formatted
// month-to-date cost string (e.g. "£34.96") for every RG in the subscription.
// Requires the caller to have Cost Management Reader (or similar) on the sub.
func (a *Azure) ResourceGroupCosts(ctx context.Context, subID string) (map[string]string, error) {
	url := fmt.Sprintf("https://management.azure.com/subscriptions/%s/providers/Microsoft.CostManagement/query?api-version=2023-11-01", subID)
	body := `{"type":"ActualCost","timeframe":"MonthToDate","dataset":{"granularity":"None","aggregation":{"totalCost":{"name":"PreTaxCost","function":"Sum"}},"grouping":[{"type":"Dimension","name":"ResourceGroupName"}]}}`
	out, err := a.az.Run(ctx,
		"rest",
		"--method", "POST",
		"--url", url,
		"--body", body,
	)
	if err != nil {
		return nil, fmt.Errorf("azure cost query: %w", err)
	}
	return parseCosts(out)
}

func parseCosts(data []byte) (map[string]string, error) {
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
		case "PreTaxCost", "Cost":
			costCol = i
		case "ResourceGroupName", "ResourceGroup":
			rgCol = i
		case "Currency":
			currencyCol = i
		}
	}
	if costCol < 0 || rgCol < 0 {
		return nil, fmt.Errorf("cost response missing expected columns")
	}
	out := make(map[string]string, len(envelope.Properties.Rows))
	for _, r := range envelope.Properties.Rows {
		if len(r) <= costCol || len(r) <= rgCol {
			continue
		}
		cost, ok := r[costCol].(float64)
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
		out[strings.ToLower(rg)] = formatCost(cost, currency)
	}
	return out, nil
}

func formatCost(amount float64, currency string) string {
	symbol := currencySymbol(currency)
	return fmt.Sprintf("%s%.2f", symbol, amount)
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
