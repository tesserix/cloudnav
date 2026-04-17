package azure

import (
	"context"
	"encoding/json"
	"fmt"
)

type Recommendation struct {
	Category     string `json:"category"`
	Impact       string `json:"impact"`
	Problem      string `json:"problem"`
	Solution     string `json:"solution"`
	ImpactedName string `json:"impacted"`
	ImpactedType string `json:"type"`
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
			Properties struct {
				Category         string `json:"category"`
				Impact           string `json:"impact"`
				ImpactedField    string `json:"impactedField"`
				ImpactedValue    string `json:"impactedValue"`
				LastUpdated      string `json:"lastUpdated"`
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
		out = append(out, Recommendation{
			Category:     v.Properties.Category,
			Impact:       v.Properties.Impact,
			Problem:      v.Properties.ShortDescription.Problem,
			Solution:     v.Properties.ShortDescription.Solution,
			ImpactedName: v.Properties.ImpactedValue,
			ImpactedType: v.Properties.ImpactedField,
			LastUpdated:  v.Properties.LastUpdated,
		})
	}
	return out, nil
}
