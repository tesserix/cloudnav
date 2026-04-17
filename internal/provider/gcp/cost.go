package gcp

import (
	"context"
	"fmt"

	"github.com/tesserix/cloudnav/internal/provider"
)

func (g *GCP) Costs(_ context.Context, _ provider.Node) (map[string]string, error) {
	return nil, fmt.Errorf("gcp per-project cost needs BigQuery billing export — see cloud.google.com/billing/docs/how-to/export-data-bigquery")
}
