package azure

import (
	"context"
	"crypto/rand"
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
	roles, err := parsePIM(out)
	if err != nil {
		return nil, err
	}
	for i := range roles {
		if name := a.subName(subIDFromScope(roles[i].Scope)); name != "" {
			roles[i].ScopeName = name
		}
	}
	return roles, nil
}

func parsePIM(data []byte) ([]provider.PIMRole, error) {
	var envelope struct {
		Value []struct {
			ID         string `json:"id"`
			Properties struct {
				Scope                           string `json:"scope"`
				PrincipalID                     string `json:"principalId"`
				RoleDefinitionID                string `json:"roleDefinitionId"`
				EndDateTime                     string `json:"endDateTime"`
				RoleEligibilityScheduleID       string `json:"roleEligibilityScheduleId"`
				LinkedRoleEligibilityScheduleID string `json:"linkedRoleEligibilityScheduleId"`
				ExpandedProperties              struct {
					RoleDefinition struct {
						DisplayName string `json:"displayName"`
						ID          string `json:"id"`
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
		roleDefID := v.Properties.RoleDefinitionID
		if roleDefID == "" {
			roleDefID = v.Properties.ExpandedProperties.RoleDefinition.ID
		}
		eligID := v.Properties.LinkedRoleEligibilityScheduleID
		if eligID == "" {
			eligID = v.Properties.RoleEligibilityScheduleID
		}
		roles = append(roles, provider.PIMRole{
			ID:               v.ID,
			RoleName:         v.Properties.ExpandedProperties.RoleDefinition.DisplayName,
			Scope:            v.Properties.Scope,
			PrincipalID:      v.Properties.PrincipalID,
			RoleDefinitionID: roleDefID,
			EligibilityID:    eligID,
			EndDateTime:      v.Properties.EndDateTime,
		})
	}
	return roles, nil
}

// ActivateRole requests activation of an eligible PIM role. durationHours is
// the requested elevation window (capped by the role's PIM policy).
func (a *Azure) ActivateRole(ctx context.Context, role provider.PIMRole, justification string, durationHours int) error {
	if justification == "" {
		return fmt.Errorf("justification is required by PIM policy")
	}
	if role.Scope == "" || role.PrincipalID == "" || role.RoleDefinitionID == "" {
		return fmt.Errorf("PIM role is missing scope / principalId / roleDefinitionId")
	}
	if durationHours <= 0 {
		durationHours = 1
	}
	guid, err := newGUID()
	if err != nil {
		return err
	}
	body := map[string]any{
		"properties": map[string]any{
			"principalId":                     role.PrincipalID,
			"roleDefinitionId":                role.RoleDefinitionID,
			"requestType":                     "SelfActivate",
			"linkedRoleEligibilityScheduleId": role.EligibilityID,
			"justification":                   justification,
			"scheduleInfo": map[string]any{
				"expiration": map[string]any{
					"type":     "AfterDuration",
					"duration": fmt.Sprintf("PT%dH", durationHours),
				},
			},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	url := fmt.Sprintf(
		"https://management.azure.com%s/providers/Microsoft.Authorization/roleAssignmentScheduleRequests/%s?api-version=2020-10-01",
		role.Scope, guid,
	)
	_, err = a.az.Run(ctx,
		"rest",
		"--method", "PUT",
		"--url", url,
		"--body", string(raw),
	)
	return err
}

// newGUID returns a random RFC 4122 v4 UUID without pulling in a dep.
func newGUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16],
	), nil
}
