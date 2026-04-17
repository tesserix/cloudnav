package gcp

import (
	"context"
	"fmt"

	"github.com/tesserix/cloudnav/internal/provider"
)

func (g *GCP) ListEligibleRoles(_ context.Context) ([]provider.PIMRole, error) {
	return nil, fmt.Errorf("gcp: no native PIM — use `gcloud projects add-iam-policy-binding <PROJECT> --role roles/<ROLE> --member user:... --condition 'expression=request.time < timestamp(...)'` for time-bound elevation, or a Just-in-Time access platform")
}

func (g *GCP) ActivateRole(_ context.Context, _ provider.PIMRole, _ string, _ int) error {
	return fmt.Errorf("gcp: activation via conditional IAM binding — run the gcloud command printed by ListEligibleRoles")
}
