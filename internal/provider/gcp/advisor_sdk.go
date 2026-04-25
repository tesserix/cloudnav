package gcp

import (
	"context"
	"strings"
	"sync"
	"time"

	recommender "cloud.google.com/go/recommender/apiv1"
	recommenderpb "cloud.google.com/go/recommender/apiv1/recommenderpb"
	"google.golang.org/api/iterator"

	"github.com/tesserix/cloudnav/internal/provider"
)

// Recommender SDK client lifecycle. Same lazy + cached-error
// pattern as the other SDK files in this package; package-scoped so
// the file owns its surface entirely.
var (
	recOnce    sync.Once
	recClient  *recommender.Client
	recInitErr error
)

func (g *GCP) recommenderClient(ctx context.Context) (*recommender.Client, error) {
	recOnce.Do(func() {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		client, err := recommender.NewClient(c)
		if err != nil {
			recInitErr = err
			return
		}
		recClient = client
	})
	return recClient, recInitErr
}

// fetchRecommenderSDK queries one recommender via the SDK and
// returns its rows. Returns (nil, false, err) on SDK-unusable
// envs so the caller falls back to gcloud.
func (g *GCP) fetchRecommenderSDK(ctx context.Context, projectID, recommenderID, category, impactedType string) ([]provider.Recommendation, bool, error) {
	client, err := g.recommenderClient(ctx)
	if err != nil || client == nil {
		return nil, false, err
	}
	parent := "projects/" + projectID + "/locations/global/recommenders/" + recommenderID
	it := client.ListRecommendations(ctx, &recommenderpb.ListRecommendationsRequest{
		Parent: parent,
	})
	out := make([]provider.Recommendation, 0, 8)
	for {
		r, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, true, err
		}
		desc := strings.TrimSpace(r.GetDescription())
		impact := mapPriority(r.GetPriority())
		// PrimaryImpact.Category is an enum on the proto; fall
		// back to the catalog category we passed in when the proto
		// didn't carry one.
		if r.GetPrimaryImpact() != nil {
			cat := r.GetPrimaryImpact().GetCategory().String()
			if cat != "" && cat != "CATEGORY_UNSPECIFIED" {
				category = humaniseRecommenderCategory(cat)
			}
		}
		var resourceID string
		if r.GetContent() != nil {
			_ = r.GetContent().GetOverview()
		}
		out = append(out, provider.Recommendation{
			Problem:      desc,
			Category:     category,
			Impact:       impact,
			ImpactedType: impactedType,
			ResourceID:   resourceID,
			LastUpdated:  r.GetLastRefreshTime().AsTime().Format(time.RFC3339),
		})
	}
	return out, true, nil
}

// mapPriority converts the SDK enum (P1/P2/P3/P4) to the impact
// labels the TUI advisor renders. Mirrors the gcloud-CLI parser.
func mapPriority(p recommenderpb.Recommendation_Priority) string {
	switch p {
	case recommenderpb.Recommendation_P1:
		return "high"
	case recommenderpb.Recommendation_P2:
		return "medium"
	case recommenderpb.Recommendation_P3, recommenderpb.Recommendation_P4:
		return "low"
	default:
		return "low"
	}
}

func humaniseRecommenderCategory(category string) string {
	switch category {
	case "COST":
		return "Cost"
	case "PERFORMANCE":
		return "Performance"
	case "SECURITY":
		return "Security"
	case "RELIABILITY":
		return "Reliability"
	case "MANAGEABILITY":
		return "Manageability"
	case "SUSTAINABILITY":
		return "Sustainability"
	default:
		return category
	}
}

func closeRecommenderClient() error {
	if recClient != nil {
		return recClient.Close()
	}
	return nil
}
