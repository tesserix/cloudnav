package azure

import (
	"context"
	"encoding/json"
	"fmt"
)

// PIMRole represents a PIM (Privileged Identity Management) role assignment
// the current user is eligible to activate.
type PIMRole struct {
	ID          string `json:"id"`
	RoleName    string `json:"roleName"`
	Scope       string `json:"scope"`
	PrincipalID string `json:"principalId"`
	EndDateTime string `json:"endDateTime,omitempty"`
}

// ListEligibleRoles returns PIM-eligible role assignments for the calling user.
func (a *Azure) ListEligibleRoles(ctx context.Context) ([]PIMRole, error) {
	url := "https://management.azure.com/providers/Microsoft.Authorization/roleEligibilityScheduleInstances?api-version=2020-10-01&$filter=asTarget()"
	out, err := a.az.Run(ctx, "rest", "--method", "GET", "--url", url)
	if err != nil {
		return nil, fmt.Errorf("list eligible PIM roles: %w", err)
	}
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
	if err := json.Unmarshal(out, &envelope); err != nil {
		return nil, fmt.Errorf("parse PIM response: %w", err)
	}
	roles := make([]PIMRole, 0, len(envelope.Value))
	for _, v := range envelope.Value {
		roles = append(roles, PIMRole{
			ID:          v.ID,
			RoleName:    v.Properties.ExpandedProperties.RoleDefinition.DisplayName,
			Scope:       v.Properties.Scope,
			PrincipalID: v.Properties.PrincipalID,
			EndDateTime: v.Properties.EndDateTime,
		})
	}
	return roles, nil
}

// Activate requests activation of the given eligible role. Duration is the
// requested hours (capped by the role's policy). Justification is required by
// most PIM policies.
//
// TODO: POST /providers/Microsoft.Authorization/roleAssignmentScheduleRequests/{guid}
// with a body derived from the eligible role — implemented in Phase 1 follow-up.
func (a *Azure) Activate(_ context.Context, role PIMRole, justification string, durationHours int) error {
	if justification == "" {
		return fmt.Errorf("justification is required by PIM policy")
	}
	if durationHours <= 0 {
		durationHours = 1
	}
	_ = role
	return fmt.Errorf("PIM activation not yet implemented — read-only listing for now")
}
