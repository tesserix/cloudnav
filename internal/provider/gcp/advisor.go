package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/tesserix/cloudnav/internal/provider"
)

// recommenderCatalog is the set of Recommender IDs we query per project when
// the user presses A on a GCP project. They're picked to match the Azure
// Advisor categories (cost / performance / security / reliability) so the
// TUI overlay renders consistently across clouds.
//
// Every entry is best-effort: if the underlying API is disabled on the
// project the call fails and we continue with the rest. The full catalog is
// at https://cloud.google.com/recommender/docs/recommenders.
var recommenderCatalog = []struct {
	id, category, impactedType string
}{
	{"google.compute.instance.MachineTypeRecommender", "Cost", "compute.Instance"},
	{"google.compute.instance.IdleResourceRecommender", "Cost", "compute.Instance"},
	{"google.compute.disk.IdleResourceRecommender", "Cost", "compute.Disk"},
	{"google.compute.address.IdleResourceRecommender", "Cost", "compute.Address"},
	{"google.compute.image.IdleResourceRecommender", "Cost", "compute.Image"},
	{"google.iam.policy.Recommender", "Security", "iam.Policy"},
	{"google.cloudsql.instance.OverprovisionedRecommender", "Cost", "sql.Instance"},
	{"google.cloudsql.instance.IdleRecommender", "Cost", "sql.Instance"},
	{"google.bigquery.table.PartitionClusterRecommender", "Performance", "bigquery.Table"},
	{"google.logging.productSuggestion.ContainerRecommender", "Reliability", "logging.Container"},
}

// Recommendations queries Google Cloud Recommender across the catalog in
// parallel for the given project. Implements provider.Advisor.
func (g *GCP) Recommendations(ctx context.Context, projectID string) ([]provider.Recommendation, error) {
	if projectID == "" {
		return nil, fmt.Errorf("gcp advisor: project id is required")
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var out []provider.Recommendation
	sem := make(chan struct{}, 6)
	errs := []string{}
	for _, r := range recommenderCatalog {
		r := r
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			recs, err := g.fetchRecommender(ctx, projectID, r.id, r.category, r.impactedType)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %s", r.id, firstLine(err.Error())))
				return
			}
			out = append(out, recs...)
		}()
	}
	wg.Wait()
	if len(out) == 0 && len(errs) > 0 {
		// Only surface an error when every recommender failed — typically
		// means Recommender API isn't enabled on this project.
		return nil, fmt.Errorf("gcp recommender returned no data — enable recommender.googleapis.com on project %s (%s)", projectID, errs[0])
	}
	return out, nil
}

func (g *GCP) fetchRecommender(ctx context.Context, projectID, recommenderID, category, impactedType string) ([]provider.Recommendation, error) {
	// SDK fast path — Recommender ListRecommendations RPC. Falls back
	// to gcloud when ADC isn't usable.
	if recs, sdkUsable, err := g.fetchRecommenderSDK(ctx, projectID, recommenderID, category, impactedType); sdkUsable && err == nil {
		return recs, nil
	}
	out, err := g.gcloud.Run(ctx,
		"recommender", "recommendations", "list",
		"--project="+projectID,
		"--location=global",
		"--recommender="+recommenderID,
		"--format=json",
	)
	if err != nil {
		return nil, err
	}
	return parseRecommenderList(out, category, impactedType)
}

type recommenderItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	LastRefresh string `json:"lastRefreshTime"`
	Priority    string `json:"priority"` // P1 / P2 / P3 / P4
	Content     struct {
		OverviewOther any `json:"overview"`
	} `json:"content"`
	PrimaryImpact struct {
		Category string `json:"category"` // COST / PERFORMANCE / SECURITY / RELIABILITY
	} `json:"primaryImpact"`
	TargetResources []string `json:"targetResources"`
}

func parseRecommenderList(data []byte, fallbackCategory, impactedType string) ([]provider.Recommendation, error) {
	var items []recommenderItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parse recommender list: %w", err)
	}
	out := make([]provider.Recommendation, 0, len(items))
	for _, it := range items {
		cat := fallbackCategory
		if c := titleize(it.PrimaryImpact.Category); c != "" {
			cat = c
		}
		target := ""
		if len(it.TargetResources) > 0 {
			target = it.TargetResources[0]
		}
		out = append(out, provider.Recommendation{
			Category:     cat,
			Impact:       priorityToImpact(it.Priority),
			Problem:      it.Description,
			Solution:     "gcloud recommender recommendations describe " + shortName(it.Name) + " --project=...",
			ImpactedName: shortName(target),
			ImpactedType: impactedType,
			ResourceID:   target,
			LastUpdated:  it.LastRefresh,
		})
	}
	return out, nil
}

// priorityToImpact maps Recommender's P1/P2/P3/P4 onto the same High/Medium/
// Low scale Azure Advisor uses.
func priorityToImpact(p string) string {
	switch strings.ToUpper(p) {
	case "P1":
		return "High"
	case "P2":
		return "Medium"
	case "P3", "P4":
		return "Low"
	default:
		return "Low"
	}
}

// titleize converts Recommender's SHOUTING_SNAKE_CASE categories into the
// Title Case form the TUI renders.
func titleize(s string) string {
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	return strings.ToUpper(lower[:1]) + lower[1:]
}

// firstLine keeps multi-line gcloud error blobs readable in a single
// aggregated status string.
func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i > 0 {
		return s[:i]
	}
	return s
}
