package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tesserix/cloudnav/internal/provider"
)

// Recommendations returns cost-efficiency recommendations for the current
// AWS scope. Implemented against Compute Optimizer (EC2 + EBS) rather than
// Trusted Advisor because TA is gated behind a Business / Enterprise
// support plan — Compute Optimizer is free and covers the majority of
// cost-wasted resources users ask about (idle / oversized EC2 and
// unattached or oversized EBS).
//
// Graceful when the account hasn't opted into Compute Optimizer: a
// read-only enrollment status check fails fast with a single-line note,
// and the overlay shows that instead of dumping a raw CLI error.
func (a *AWS) Recommendations(ctx context.Context, scopeID string) ([]provider.Recommendation, error) {
	if !a.computeOptimizerEnabled(ctx) {
		return []provider.Recommendation{{
			Category: "Cost",
			Impact:   "Medium",
			Problem:  "AWS Compute Optimizer isn't enrolled on this account",
			Solution: "Run: aws compute-optimizer update-enrollment-status --status Active  (then wait ~12h for the first recommendations).",
		}}, nil
	}

	out := []provider.Recommendation{}
	out = append(out, a.ec2Recommendations(ctx)...)
	out = append(out, a.ebsRecommendations(ctx)...)
	out = append(out, a.costAnomalies(ctx)...)
	return out, nil
}

// computeOptimizerEnabled reads the enrollment status without consuming
// the scope — used as a cheap gate so we don't surface raw "not opted in"
// errors for every recommendations call.
func (a *AWS) computeOptimizerEnabled(ctx context.Context) bool {
	out, err := a.aws.Run(ctx, "compute-optimizer", "get-enrollment-status", "--output", "json")
	if err != nil {
		return false
	}
	var env struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return false
	}
	return strings.EqualFold(env.Status, "Active")
}

// ec2Recommendations surfaces under/over-provisioned EC2 instances. We
// only bubble up instances where Compute Optimizer has a concrete finding
// (NotOptimized | Underprovisioned | Overprovisioned); "Optimized"
// instances are expected.
func (a *AWS) ec2Recommendations(ctx context.Context) []provider.Recommendation {
	out, err := a.aws.Run(ctx,
		"compute-optimizer", "get-ec2-instance-recommendations",
		"--output", "json",
	)
	if err != nil {
		return nil
	}
	var env struct {
		InstanceRecommendations []struct {
			InstanceArn     string   `json:"instanceArn"`
			InstanceName    string   `json:"instanceName"`
			CurrentInstance string   `json:"currentInstanceType"`
			Finding         string   `json:"finding"`
			FindingReasons  []string `json:"findingReasonCodes"`
			Recommendations []struct {
				InstanceType string `json:"instanceType"`
				Rank         int    `json:"rank"`
			} `json:"recommendationOptions"`
		} `json:"instanceRecommendations"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return nil
	}
	recs := make([]provider.Recommendation, 0, len(env.InstanceRecommendations))
	for _, r := range env.InstanceRecommendations {
		if strings.EqualFold(r.Finding, "Optimized") {
			continue
		}
		better := ""
		if len(r.Recommendations) > 0 {
			better = r.Recommendations[0].InstanceType
		}
		reasons := strings.Join(r.FindingReasons, ", ")
		solution := fmt.Sprintf("Resize to %s (Compute Optimizer recommendation).", better)
		if better == "" {
			solution = "See Compute Optimizer in the console for the recommended size."
		}
		impact := "Medium"
		if strings.EqualFold(r.Finding, "Underprovisioned") {
			impact = "High" // throttled workloads cost reliability, not just money
		}
		recs = append(recs, provider.Recommendation{
			Category:     "Cost",
			Impact:       impact,
			Problem:      fmt.Sprintf("%s: %s (%s) — %s", r.Finding, nameOrArn(r.InstanceName, r.InstanceArn), r.CurrentInstance, reasons),
			Solution:     solution,
			ImpactedName: nameOrArn(r.InstanceName, r.InstanceArn),
			ImpactedType: "ec2:instance",
			ResourceID:   r.InstanceArn,
		})
	}
	return recs
}

// ebsRecommendations surfaces EBS volumes that should be resized or
// switched to a different type (gp2→gp3 is the common cost win). Also
// catches unattached volumes on supported AWS CLI versions (some builds
// don't include the attachmentState field).
func (a *AWS) ebsRecommendations(ctx context.Context) []provider.Recommendation {
	out, err := a.aws.Run(ctx,
		"compute-optimizer", "get-ebs-volume-recommendations",
		"--output", "json",
	)
	if err != nil {
		return nil
	}
	var env struct {
		VolumeRecommendations []struct {
			VolumeArn     string `json:"volumeArn"`
			CurrentConfig struct {
				VolumeType string `json:"volumeType"`
				VolumeSize int    `json:"volumeSize"`
			} `json:"currentConfiguration"`
			Finding         string   `json:"finding"`
			FindingReasons  []string `json:"findingReasonCodes"`
			Recommendations []struct {
				Configuration struct {
					VolumeType string `json:"volumeType"`
					VolumeSize int    `json:"volumeSize"`
				} `json:"configuration"`
				Rank int `json:"rank"`
			} `json:"volumeRecommendationOptions"`
		} `json:"volumeRecommendations"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return nil
	}
	recs := make([]provider.Recommendation, 0, len(env.VolumeRecommendations))
	for _, r := range env.VolumeRecommendations {
		if strings.EqualFold(r.Finding, "Optimized") {
			continue
		}
		better := ""
		if len(r.Recommendations) > 0 {
			cfg := r.Recommendations[0].Configuration
			better = fmt.Sprintf("%s %dGiB", cfg.VolumeType, cfg.VolumeSize)
		}
		reasons := strings.Join(r.FindingReasons, ", ")
		solution := fmt.Sprintf("Switch to %s (Compute Optimizer recommendation).", better)
		if better == "" {
			solution = "See Compute Optimizer in the console for the recommended config."
		}
		recs = append(recs, provider.Recommendation{
			Category:     "Cost",
			Impact:       "Medium",
			Problem:      fmt.Sprintf("%s: %s %s %dGiB — %s", r.Finding, shortArn(r.VolumeArn), r.CurrentConfig.VolumeType, r.CurrentConfig.VolumeSize, reasons),
			Solution:     solution,
			ImpactedName: shortArn(r.VolumeArn),
			ImpactedType: "ebs:volume",
			ResourceID:   r.VolumeArn,
		})
	}
	return recs
}

// costAnomalies pulls the last 7 days of anomalies from Cost Anomaly
// Detection. The service is free and gives a high-signal alerting layer
// on top of the other recommendations.
func (a *AWS) costAnomalies(ctx context.Context) []provider.Recommendation {
	end := time.Now().UTC()
	start := end.AddDate(0, 0, -7)
	out, err := a.aws.Run(ctx,
		"ce", "get-anomalies",
		"--date-interval", fmt.Sprintf("StartDate=%s,EndDate=%s", start.Format("2006-01-02"), end.Format("2006-01-02")),
		"--output", "json",
	)
	if err != nil {
		return nil
	}
	var env struct {
		Anomalies []struct {
			RootCauses []struct {
				Service string `json:"Service"`
				Region  string `json:"Region"`
			} `json:"RootCauses"`
			Impact struct {
				MaxImpact          float64 `json:"MaxImpact"`
				TotalImpact        float64 `json:"TotalImpact"`
				TotalActualSpend   float64 `json:"TotalActualSpend"`
				TotalExpectedSpend float64 `json:"TotalExpectedSpend"`
			} `json:"Impact"`
			AnomalyStartDate string `json:"AnomalyStartDate"`
		} `json:"Anomalies"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return nil
	}
	recs := make([]provider.Recommendation, 0, len(env.Anomalies))
	for _, an := range env.Anomalies {
		service := "multiple services"
		region := ""
		if len(an.RootCauses) > 0 {
			service = an.RootCauses[0].Service
			region = an.RootCauses[0].Region
		}
		problem := fmt.Sprintf("%s in %s: spent $%.2f (expected $%.2f, +$%.2f)",
			service, strOr(region, "unknown region"),
			an.Impact.TotalActualSpend, an.Impact.TotalExpectedSpend, an.Impact.TotalImpact)
		recs = append(recs, provider.Recommendation{
			Category:     "Cost",
			Impact:       anomalyImpactBadge(an.Impact.TotalImpact),
			Problem:      problem,
			Solution:     "Investigate recent changes to this service / region and tag any new resources for chargeback.",
			ImpactedName: service,
			ImpactedType: "cost-anomaly",
			LastUpdated:  an.AnomalyStartDate,
		})
	}
	return recs
}

func anomalyImpactBadge(delta float64) string {
	switch {
	case delta > 500:
		return "High"
	case delta > 100:
		return "Medium"
	default:
		return "Low"
	}
}

func nameOrArn(name, arn string) string {
	if name != "" {
		return name
	}
	return shortArn(arn)
}

// shortArn trims an ARN to its name segment for readable rec rows.
func shortArn(arn string) string {
	return nameFromARN(arn)
}

func strOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// Ensure AWS implements the Advisor interface at compile time.
var _ provider.Advisor = (*AWS)(nil)
