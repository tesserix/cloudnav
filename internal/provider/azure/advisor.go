package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type Recommendation struct {
	Category     string `json:"category"`
	Impact       string `json:"impact"`
	Problem      string `json:"problem"`
	Solution     string `json:"solution"`
	ImpactedName string `json:"impacted"`
	ImpactedType string `json:"type"`
	ResourceID   string `json:"resourceId"`
	LastUpdated  string `json:"lastUpdated"`
}

func (a *Azure) Recommendations(ctx context.Context, subID string) ([]Recommendation, error) {
	url := fmt.Sprintf(
		"https://management.azure.com/subscriptions/%s/providers/Microsoft.Advisor/recommendations?api-version=2023-01-01",
		subID,
	)
	out, err := a.getJSONForSub(ctx, subID, url)
	if err != nil {
		return nil, fmt.Errorf("azure advisor: %w", err)
	}
	return parseRecommendations(out)
}

func parseRecommendations(data []byte) ([]Recommendation, error) {
	var env struct {
		Value []struct {
			ID         string `json:"id"`
			Properties struct {
				Category         string `json:"category"`
				Impact           string `json:"impact"`
				ImpactedField    string `json:"impactedField"`
				ImpactedValue    string `json:"impactedValue"`
				LastUpdated      string `json:"lastUpdated"`
				ResourceMetadata struct {
					ResourceID string `json:"resourceId"`
				} `json:"resourceMetadata"`
				ShortDescription struct {
					Problem  string `json:"problem"`
					Solution string `json:"solution"`
				} `json:"shortDescription"`
			} `json:"properties"`
		} `json:"value"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse advisor response: %w", err)
	}
	out := make([]Recommendation, 0, len(env.Value))
	for _, v := range env.Value {
		resID := v.Properties.ResourceMetadata.ResourceID
		if resID == "" {
			// Some advisor responses omit resourceMetadata. The recommendation
			// id encodes the target scope: /subscriptions/.../providers/... —
			// everything up to /providers/Microsoft.Advisor is the scope.
			if i := strings.Index(strings.ToLower(v.ID), "/providers/microsoft.advisor"); i > 0 {
				resID = v.ID[:i]
			}
		}
		out = append(out, Recommendation{
			Category:     v.Properties.Category,
			Impact:       v.Properties.Impact,
			Problem:      v.Properties.ShortDescription.Problem,
			Solution:     v.Properties.ShortDescription.Solution,
			ImpactedName: v.Properties.ImpactedValue,
			ImpactedType: v.Properties.ImpactedField,
			ResourceID:   resID,
			LastUpdated:  v.Properties.LastUpdated,
		})
	}
	return out, nil
}
