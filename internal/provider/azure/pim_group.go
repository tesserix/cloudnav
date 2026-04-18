package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/tesserix/cloudnav/internal/provider"
)

const groupAccessMember = "member"

// listGroupPIM returns PIM-for-Groups eligible memberships ("Groups" tab in
// the portal) for the signed-in user.
func (a *Azure) listGroupPIM(ctx context.Context, tid string, client *http.Client) ([]provider.PIMRole, error) {
	token, err := a.tenantTokenFor(ctx, tid, graphResource)
	if err != nil {
		return nil, err
	}
	oid, err := a.signedInOID(ctx, tid, client, token)
	if err != nil {
		return nil, err
	}
	eligURL := "https://graph.microsoft.com/v1.0/identityGovernance/privilegedAccess/group/eligibilityScheduleInstances?$expand=group&$filter=principalId%20eq%20'" + oid + "'"
	activeURL := "https://graph.microsoft.com/v1.0/identityGovernance/privilegedAccess/group/assignmentScheduleInstances?$expand=group&$filter=principalId%20eq%20'" + oid + "'"

	body, err := fetchWithToken(ctx, client, eligURL, token)
	if err != nil {
		return nil, fmt.Errorf("group PIM eligible: %w", err)
	}
	roles, err := parseGroupEligible(body, tid, oid)
	if err != nil {
		return nil, err
	}
	activeBody, _ := fetchWithToken(ctx, client, activeURL, token)
	active := parseGroupActive(activeBody)
	for i := range roles {
		key := roles[i].GroupID + "|" + roles[i].RoleName
		if until, ok := active[key]; ok {
			roles[i].Active = true
			roles[i].ActiveUntil = until
		}
	}
	return roles, nil
}

type groupEnvelope struct {
	Value []struct {
		ID          string `json:"id"`
		PrincipalID string `json:"principalId"`
		GroupID     string `json:"groupId"`
		AccessID    string `json:"accessId"` // groupAccessMember or "owner"
		EndDateTime string `json:"endDateTime"`
		Group       struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"group"`
	} `json:"value"`
}

func parseGroupEligible(body []byte, tid, oid string) ([]provider.PIMRole, error) {
	var env groupEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("parse group PIM: %w", err)
	}
	out := make([]provider.PIMRole, 0, len(env.Value))
	for _, v := range env.Value {
		access := v.AccessID
		if access == "" {
			access = groupAccessMember
		}
		displayName := v.Group.DisplayName
		if displayName == "" {
			displayName = v.GroupID
		}
		out = append(out, provider.PIMRole{
			ID:               v.ID,
			RoleName:         access, // groupAccessMember / "owner"
			Scope:            "/groups/" + v.GroupID,
			ScopeName:        displayName,
			TenantID:         tid,
			PrincipalID:      ifEmpty(v.PrincipalID, oid),
			GroupID:          v.GroupID,
			RoleDefinitionID: "group-" + access,
			EndDateTime:      v.EndDateTime,
			Source:           "group",
			MaxDurationHours: 8,
		})
	}
	return out, nil
}

func parseGroupActive(body []byte) map[string]string {
	out := map[string]string{}
	if len(body) == 0 {
		return out
	}
	var env groupEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return out
	}
	for _, v := range env.Value {
		access := v.AccessID
		if access == "" {
			access = groupAccessMember
		}
		out[v.GroupID+"|"+access] = v.EndDateTime
	}
	return out
}

// activateGroupRole activates PIM-for-Groups membership / ownership via Graph
// assignmentScheduleRequests.
func (a *Azure) activateGroupRole(ctx context.Context, role provider.PIMRole, justification string, durationHours int) error {
	if role.GroupID == "" {
		return fmt.Errorf("group PIM role is missing groupId")
	}
	token, err := a.tenantTokenFor(ctx, role.TenantID, graphResource)
	if err != nil {
		return err
	}
	access := role.RoleName
	if access == "" {
		access = groupAccessMember
	}
	body := map[string]any{
		"accessId":      access,
		"principalId":   role.PrincipalID,
		"groupId":       role.GroupID,
		"action":        "selfActivate",
		"justification": justification,
		"scheduleInfo": map[string]any{
			"startDateTime": time.Now().UTC().Format(time.RFC3339),
			"expiration": map[string]any{
				"type":     "AfterDuration",
				"duration": fmt.Sprintf("PT%dH", durationHours),
			},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return graphPOST(ctx, token, "https://graph.microsoft.com/v1.0/identityGovernance/privilegedAccess/group/assignmentScheduleRequests", raw)
}
