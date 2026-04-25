package aws

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	co "github.com/aws/aws-sdk-go-v2/service/computeoptimizer"
	cotypes "github.com/aws/aws-sdk-go-v2/service/computeoptimizer/types"

	"github.com/tesserix/cloudnav/internal/provider"
)

// Compute Optimizer SDK lifecycle. Used both as the "is the API
// even enabled?" probe (cheap) and for the EC2 / EBS recommendation
// fetches.
var (
	coOnce    sync.Once
	coClient  *co.Client
	coInitErr error
)

func (a *AWS) computeOptimizerClient(ctx context.Context) (*co.Client, error) {
	coOnce.Do(func() {
		cfg, err := sdkConfig(ctx)
		if err != nil {
			coInitErr = err
			return
		}
		coClient = co.NewFromConfig(cfg)
	})
	return coClient, coInitErr
}

// computeOptimizerEnabledSDK is the SDK fast path for the
// enrollment-status gate. Returns (enabled, true) on a clean
// answer, (false, false) when the SDK isn't usable so the caller
// can fall back to the CLI probe.
func (a *AWS) computeOptimizerEnabledSDK(ctx context.Context) (bool, bool) {
	client, err := a.computeOptimizerClient(ctx)
	if err != nil || client == nil {
		return false, false
	}
	out, err := client.GetEnrollmentStatus(ctx, &co.GetEnrollmentStatusInput{})
	if err != nil {
		return false, false
	}
	return out.Status == cotypes.StatusActive, true
}

// fetchEC2RecommendationsSDK queries Compute Optimizer for
// EC2 instance findings. Mirrors the CLI parser shape so the TUI's
// advisor card renders identically across paths.
//
// The Compute Optimizer SDK doesn't ship a generated paginator
// for GetEC2InstanceRecommendations (it's an older API surface),
// so we walk NextToken manually. Same shape as the SDK's typed
// paginators on every other service in this package.
func (a *AWS) fetchEC2RecommendationsSDK(ctx context.Context) ([]provider.Recommendation, bool, error) {
	client, err := a.computeOptimizerClient(ctx)
	if err != nil || client == nil {
		return nil, false, err
	}
	out := make([]provider.Recommendation, 0, 16)
	var nextToken *string
	for {
		page, err := client.GetEC2InstanceRecommendations(ctx, &co.GetEC2InstanceRecommendationsInput{
			NextToken: nextToken,
		})
		if err != nil {
			return nil, true, err
		}
		for _, r := range page.InstanceRecommendations {
			finding := string(r.Finding)
			if strings.EqualFold(finding, "Optimized") {
				continue
			}
			better := ""
			if len(r.RecommendationOptions) > 0 {
				better = aws.ToString(r.RecommendationOptions[0].InstanceType)
			}
			reasons := joinFindingReasons(r.FindingReasonCodes)
			impact := impactMedium
			if strings.EqualFold(finding, "Underprovisioned") {
				impact = impactHigh
			}
			solution := "See Compute Optimizer in the console for the recommended size."
			if better != "" {
				solution = "Resize to " + better + " (Compute Optimizer recommendation)."
			}
			arn := aws.ToString(r.InstanceArn)
			name := aws.ToString(r.InstanceName)
			if name == "" {
				name = arn
			}
			out = append(out, provider.Recommendation{
				Category:     "Cost",
				Impact:       impact,
				Problem:      finding + ": " + name + " (" + aws.ToString(r.CurrentInstanceType) + ") — " + reasons,
				Solution:     solution,
				ImpactedName: name,
				ImpactedType: "ec2:instance",
				ResourceID:   arn,
			})
		}
		if page.NextToken == nil || *page.NextToken == "" {
			break
		}
		nextToken = page.NextToken
	}
	return out, true, nil
}

func joinFindingReasons(codes []cotypes.InstanceRecommendationFindingReasonCode) string {
	if len(codes) == 0 {
		return ""
	}
	parts := make([]string, 0, len(codes))
	for _, c := range codes {
		parts = append(parts, string(c))
	}
	return strings.Join(parts, ", ")
}

// fetchCostAnomaliesSDK turns Cost Explorer anomalies into the
// provider.Recommendation shape. Wraps fetchAnomaliesSDK from
// cost_sdk.go.
func (a *AWS) fetchCostAnomaliesSDK(ctx context.Context) ([]provider.Recommendation, bool, error) {
	to := time.Now().UTC()
	from := to.AddDate(0, 0, -30) // last 30 days, matches CLI parser
	anomalies, sdkUsable, err := a.fetchAnomaliesSDK(ctx, from, to)
	if !sdkUsable || err != nil {
		return nil, sdkUsable, err
	}
	out := make([]provider.Recommendation, 0, len(anomalies))
	for _, an := range anomalies {
		impact := impactMedium
		if an.Impact != nil && an.Impact.TotalImpact > 1000 {
			impact = impactHigh
		}
		out = append(out, provider.Recommendation{
			Category:     "Cost",
			Impact:       impact,
			Problem:      "Cost anomaly: " + aws.ToString(an.MonitorArn),
			Solution:     "Investigate the anomaly window in the Cost Explorer console.",
			ImpactedName: aws.ToString(an.AnomalyId),
			ImpactedType: "ce:anomaly",
		})
	}
	return out, true, nil
}
