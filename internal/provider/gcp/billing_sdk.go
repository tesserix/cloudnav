package gcp

import (
	"context"
	"strings"
	"sync"
	"time"

	billing "cloud.google.com/go/billing/apiv1"
	billingpb "cloud.google.com/go/billing/apiv1/billingpb"
)

// Cloud Billing SDK lifecycle.
var (
	billingClientOnce sync.Once
	billingClient     *billing.CloudBillingClient
	billingClientErr  error
)

func (g *GCP) cloudBillingClient(ctx context.Context) (*billing.CloudBillingClient, error) {
	billingClientOnce.Do(func() {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		client, err := billing.NewCloudBillingClient(c)
		if err != nil {
			billingClientErr = err
			return
		}
		billingClient = client
	})
	return billingClient, billingClientErr
}

// projectBillingAccountSDK returns the billing-account name bound
// to a project ("billingAccounts/0XXXXX-YYYYYY-ZZZZZZ"). Returns
// (nil, false, err) when the SDK isn't usable so the caller falls
// back to `gcloud billing projects describe`.
//
// Used by the cost auto-detect path (Costs() in cost.go) to find
// the canonical export table without parsing CLI output.
func (g *GCP) projectBillingAccountSDK(ctx context.Context, projectID string) (string, bool, error) {
	client, err := g.cloudBillingClient(ctx)
	if err != nil || client == nil {
		return "", false, err
	}
	resp, err := client.GetProjectBillingInfo(ctx, &billingpb.GetProjectBillingInfoRequest{
		Name: "projects/" + projectID,
	})
	if err != nil {
		return "", true, err
	}
	// API returns "billingAccounts/<id>"; the cost-table builder
	// wants the bare id with dashes-as-underscores so we strip the
	// prefix here.
	return strings.TrimPrefix(resp.GetBillingAccountName(), "billingAccounts/"), true, nil
}

func closeCloudBillingClient() error {
	if billingClient != nil {
		return billingClient.Close()
	}
	return nil
}
