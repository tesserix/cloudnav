package azure

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tesserix/cloudnav/internal/provider"
)

// ListEligibleRoles returns PIM-eligible role assignments for the calling user.
func (a *Azure) ListEligibleRoles(ctx context.Context) ([]provider.PIMRole, error) {
	url := "https://management.azure.com/providers/Microsoft.Authorization/roleEligibilityScheduleInstances?api-version=2020-10-01&$filter=asTarget()"
	out, err := a.az.Run(ctx, "rest", "--method", "GET", "--url", url)
	if err != nil {
		return nil, fmt.Errorf("list eligible PIM roles: %w", err)
	}
	return parsePIM(out)
}

func parsePIM(data []byte) ([]provider.PIMRole, error) {
	var envelope struct {
		Value []struct {
			ID         string `json:"id"`
			Properties struct {
				Scope              string `json:"scope"`
				PrincipalID        string `json:"principalId"`
				EndDateTime        string `json:"endDateTime"`
				ExpandedProperties struct {
					RoleDefinition struct {
						DisplayName string `json:"displayName"`
					} `json:"roleDefinition"`
				} `json:"expandedProperties"`
			} `json:"properties"`
		} `json:"value"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("parse PIM response: %w", err)
	}
	roles := make([]provider.PIMRole, 0, len(envelope.Value))
	for _, v := range envelope.Value {
		roles = append(roles, provider.PIMRole{
			ID:          v.ID,
			RoleName:    v.Properties.ExpandedProperties.RoleDefinition.DisplayName,
			Scope:       v.Properties.Scope,
			PrincipalID: v.Properties.PrincipalID,
			EndDateTime: v.Properties.EndDateTime,
		})
	}
	return roles, nil
}

// Activate requests activation of the given eligible role. Planned for a
// follow-up — currently listing is read-only.
//
// TODO: POST /providers/Microsoft.Authorization/roleAssignmentScheduleRequests/{guid}
func (a *Azure) Activate(_ context.Context, role provider.PIMRole, justification string, durationHours int) error {
	_ = role
	_ = durationHours
	if justification == "" {
		return fmt.Errorf("justification is required by PIM policy")
	}
	return fmt.Errorf("PIM activation not yet implemented — read-only listing for now")
}
