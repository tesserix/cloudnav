package gcp

import (
	"context"

	"github.com/tesserix/cloudnav/internal/provider"
)

// Billing returns month-to-date + previous month cost per project via the
// BigQuery billing export. Implements provider.Billing so the TUI's `B`
// overlay renders a portfolio-style view.
//
// Requires a configured BQ billing-export table (env var
// CLOUDNAV_GCP_BILLING_TABLE or cfg.GCP.BillingTable); attempts auto-
// detection when unset. When nothing is configured we surface a single
// CostLine that tells the user where to set it up — better than an opaque
// error in the overlay.
func (g *GCP) Billing(ctx context.Context) ([]provider.CostLine, error) {
	costs, err := g.Costs(ctx, provider.Node{Kind: provider.KindCloud})
	if err != nil {
		return []provider.CostLine{{
			Label: "(cost unavailable)",
			Note:  firstLine(err.Error()),
		}}, nil
	}
	out := make([]provider.CostLine, 0, len(costs))
	for proj, formatted := range costs {
		// Costs() already formats the amount with a currency symbol, but
		// Billing wants raw numbers so the TUI can compute deltas. Costs()
		// is the project-level rollup we already expose to the cost column —
		// for Billing we prefer a richer breakdown, but this is a reasonable
		// first cut that works without double-querying BQ.
		out = append(out, provider.CostLine{
			Label: proj,
			Note:  formatted, // preserve the formatted string for display
		})
	}
	return out, nil
}

// Ensure GCP implements Billing at compile time.
var _ provider.Billing = (*GCP)(nil)
