package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/tesserix/cloudnav/internal/provider"
)

const (
	graphResource = "https://graph.microsoft.com/"
	graphBase     = "https://graph.microsoft.com/v1.0"
)

// listEntraPIM returns directory-role eligibilities ("Microsoft Entra roles"
// in the portal) for the signed-in user on the given tenant. Active
// assignments are merged so elevated roles render as ACTIVE in the UI.
func (a *Azure) listEntraPIM(ctx context.Context, tid string, client *http.Client) ([]provider.PIMRole, error) {
	token, err := a.tenantTokenFor(ctx, tid, graphResource)
	if err != nil {
		return nil, err
	}
	oid, err := a.signedInOID(ctx, tid, client, token)
	if err != nil {
		return nil, err
	}
	eligURL := "https://graph.microsoft.com/v1.0/roleManagement/directory/roleEligibilityScheduleInstances?$expand=roleDefinition&$filter=principalId%20eq%20'" + oid + "'"
	activeURL := "https://graph.microsoft.com/v1.0/roleManagement/directory/roleAssignmentScheduleInstances?$expand=roleDefinition&$filter=principalId%20eq%20'" + oid + "'"

	body, err := fetchWithToken(ctx, client, eligURL, token)
	if err != nil {
		return nil, fmt.Errorf("entra eligible roles: %w", err)
	}
	roles, err := parseEntraEligible(body, tid, oid)
	if err != nil {
		return nil, err
	}
	activeBody, _ := fetchWithToken(ctx, client, activeURL, token)
	active := parseEntraActive(activeBody)
	for i := range roles {
		if until, ok := active[roles[i].RoleDefinitionID+"|"+roles[i].Scope]; ok {
			roles[i].Active = true
			roles[i].ActiveUntil = until
		}
	}
	return roles, nil
}

type entraEnvelope struct {
	Value []struct {
		ID               string `json:"id"`
		PrincipalID      string `json:"principalId"`
		RoleDefinitionID string `json:"roleDefinitionId"`
		DirectoryScopeID string `json:"directoryScopeId"`
		EndDateTime      string `json:"endDateTime"`
		RoleDefinition   struct {
			DisplayName string `json:"displayName"`
			ID          string `json:"id"`
		} `json:"roleDefinition"`
	} `json:"value"`
}

func parseEntraEligible(body []byte, tid, oid string) ([]provider.PIMRole, error) {
	var env entraEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("parse entra eligible: %w", err)
	}
	out := make([]provider.PIMRole, 0, len(env.Value))
	for _, v := range env.Value {
		scope := v.DirectoryScopeID
		if scope == "" || scope == "/" {
			scope = "/"
		}
		out = append(out, provider.PIMRole{
			ID:               v.ID,
			RoleName:         v.RoleDefinition.DisplayName,
			Scope:            scope,
			ScopeName:        entraScopeName(scope),
			TenantID:         tid,
			PrincipalID:      ifEmpty(v.PrincipalID, oid),
			RoleDefinitionID: v.RoleDefinitionID,
			EndDateTime:      v.EndDateTime,
			Source:           "entra",
			MaxDurationHours: 8,
		})
	}
	return out, nil
}

func parseEntraActive(body []byte) map[string]string {
	out := map[string]string{}
	if len(body) == 0 {
		return out
	}
	var env entraEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return out
	}
	for _, v := range env.Value {
		scope := v.DirectoryScopeID
		if scope == "" {
			scope = "/"
		}
		out[v.RoleDefinitionID+"|"+scope] = v.EndDateTime
	}
	return out
}

// activateEntraRole requests self-activation of an Entra directory role via
// the Graph roleAssignmentScheduleRequests endpoint.
func (a *Azure) activateEntraRole(ctx context.Context, role provider.PIMRole, justification string, durationHours int) error {
	if role.RoleDefinitionID == "" {
		return fmt.Errorf("entra PIM role is missing roleDefinitionId")
	}
	token, err := a.tenantTokenFor(ctx, role.TenantID, graphResource)
	if err != nil {
		return err
	}
	scope := role.Scope
	if scope == "" {
		scope = "/"
	}
	body := map[string]any{
		"action":           "selfActivate",
		"principalId":      role.PrincipalID,
		"roleDefinitionId": role.RoleDefinitionID,
		"directoryScopeId": scope,
		"justification":    justification,
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
	return graphPOST(ctx, token, "https://graph.microsoft.com/v1.0/roleManagement/directory/roleAssignmentScheduleRequests", raw)
}

// signedInOID returns the object-id of the token's owner by calling
// Graph /me. Cached per-tenant for the lifetime of the process.
func (a *Azure) signedInOID(ctx context.Context, tid string, client *http.Client, token string) (string, error) {
	a.mu.RLock()
	oid := a.signedInOIDs[tid]
	a.mu.RUnlock()
	if oid != "" {
		return oid, nil
	}
	body, err := fetchWithToken(ctx, client, "https://graph.microsoft.com/v1.0/me?$select=id", token)
	if err != nil {
		return "", fmt.Errorf("graph /me: %w", err)
	}
	var me struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &me); err != nil {
		return "", err
	}
	if me.ID == "" {
		return "", fmt.Errorf("graph /me: empty id")
	}
	a.mu.Lock()
	if a.signedInOIDs == nil {
		a.signedInOIDs = map[string]string{}
	}
	a.signedInOIDs[tid] = me.ID
	a.mu.Unlock()
	return me.ID, nil
}

func entraScopeName(scope string) string {
	if scope == "/" || scope == "" {
		return "Directory"
	}
	if strings.HasPrefix(scope, "/administrativeUnits/") {
		return "AU " + strings.TrimPrefix(scope, "/administrativeUnits/")
	}
	return scope
}

func ifEmpty(a, b string) string {
	if a == "" {
		return b
	}
	return a
}
