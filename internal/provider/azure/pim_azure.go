package azure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/tesserix/cloudnav/internal/provider"
)

const (
	armResource = "https://management.azure.com/"

	armEligURL   = armResource + "providers/Microsoft.Authorization/roleEligibilityScheduleInstances?api-version=2020-10-01&$filter=asTarget()"
	armActiveURL = armResource + "providers/Microsoft.Authorization/roleAssignmentScheduleInstances?api-version=2020-10-01&$filter=asTarget()"
)

// listAzurePIM fetches Azure resource RBAC eligibilities for the tenant,
// merges active assignments, and resolves the per-role activation max from
// the role-management policy.
func (a *Azure) listAzurePIM(ctx context.Context, tid string, client *http.Client) ([]provider.PIMRole, error) {
	token, err := a.tenantToken(ctx, tid)
	if err != nil {
		return nil, err
	}
	body, err := fetchWithToken(ctx, client, armEligURL, token)
	if err != nil {
		return nil, err
	}
	roles, err := parseAzurePIM(body)
	if err != nil {
		return nil, err
	}
	activeBody, _ := fetchWithToken(ctx, client, armActiveURL, token)
	active := parseActiveAssignments(activeBody)

	unique := map[string]struct{ scope, roleDef string }{}
	for _, r := range roles {
		key := r.Scope + "|" + r.RoleDefinitionID
		if _, ok := unique[key]; !ok {
			unique[key] = struct{ scope, roleDef string }{r.Scope, r.RoleDefinitionID}
		}
	}
	policyMax := fetchPolicyMaxes(ctx, client, token, unique)

	out := make([]provider.PIMRole, 0, len(roles))
	for _, r := range roles {
		r.TenantID = tid
		r.Source = "azure"
		if name := a.subName(subIDFromScope(r.Scope)); name != "" {
			r.ScopeName = name
		}
		if until, ok := active[strings.ToLower(r.RoleDefinitionID+"|"+r.Scope)]; ok {
			r.Active = true
			r.ActiveUntil = until
		}
		r.MaxDurationHours = policyMax[r.Scope+"|"+r.RoleDefinitionID]
		out = append(out, r)
	}
	return out, nil
}

// activateAzureRole requests self-activation of an Azure resource RBAC PIM
// role via ARM roleAssignmentScheduleRequests, using the role's home tenant
// token so cross-tenant activations succeed.
func (a *Azure) activateAzureRole(ctx context.Context, role provider.PIMRole, justification string, durationHours int) error {
	if role.Scope == "" || role.PrincipalID == "" || role.RoleDefinitionID == "" {
		return fmt.Errorf("PIM role is missing scope / principalId / roleDefinitionId")
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
	if subID := subIDFromScope(role.Scope); subID != "" {
		_, err := a.putJSONForSub(ctx, subID, url, raw)
		return err
	}
	// Management-group or tenant-root scope: fall back to tenant-scoped PUT.
	return a.armPUT(ctx, role.TenantID, url, raw)
}

// armPUT is used for activation at scopes that don't belong to a single
// subscription (e.g. management groups). Runs through doWithRetry so a
// transient 429 / 503 from ARM doesn't fail a self-elevation the user
// is watching live.
func (a *Azure) armPUT(ctx context.Context, tenantID, url string, body []byte) error {
	token, err := a.tenantToken(ctx, tenantID)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := doWithRetry(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		// Error message first so the status bar shows Azure's actual
		// reason (RoleAssignmentRequestPolicyValidationFailed, scope
		// mismatch, etc.) instead of a long ARM URL that gets clipped.
		return fmt.Errorf("%s [HTTP %d on PIM activate]", trimAPIErr(raw), resp.StatusCode)
	}
	return nil
}

// noErrorBody is the placeholder trimAPIErr returns when the raw
// response body is empty — the caller wraps this in a Go error so
// users still see something non-blank when Azure replies with 4xx
// and no payload.
const noErrorBody = "(no error body)"

// trimAPIErr pulls the "message" out of an Azure JSON error envelope
// when present, falling back to the raw body otherwise. Azure wraps
// errors as {"error":{"code":"X","message":"Y"}} — the message is what
// the user actually needs.
func trimAPIErr(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return noErrorBody
	}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err == nil && env.Error.Message != "" {
		if env.Error.Code != "" {
			return env.Error.Code + ": " + env.Error.Message
		}
		return env.Error.Message
	}
	// Raw body wasn't a standard envelope. Shorten so the status bar
	// stays readable — the detail overlay can render the full thing.
	if len(s) > 240 {
		s = s[:240] + "…"
	}
	return s
}

// parseAzurePIM parses the ARM roleEligibilityScheduleInstances response.
// Exported for testing via the wrapper in pim_test.go.
func parseAzurePIM(data []byte) ([]provider.PIMRole, error) {
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

// parseActiveAssignments indexes active-assignment expiries by the
// (roleDefinitionId|scope) key so list can flip Active on the matching row.
func parseActiveAssignments(body []byte) map[string]string {
	out := map[string]string{}
	if len(body) == 0 {
		return out
	}
	var env struct {
		Value []struct {
			Properties struct {
				Scope              string `json:"scope"`
				RoleDefinitionID   string `json:"roleDefinitionId"`
				EndDateTime        string `json:"endDateTime"`
				ExpandedProperties struct {
					RoleDefinition struct {
						ID string `json:"id"`
					} `json:"roleDefinition"`
				} `json:"expandedProperties"`
			} `json:"properties"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return out
	}
	for _, v := range env.Value {
		roleDef := v.Properties.RoleDefinitionID
		if roleDef == "" {
			roleDef = v.Properties.ExpandedProperties.RoleDefinition.ID
		}
		if roleDef == "" || v.Properties.Scope == "" {
			continue
		}
		out[strings.ToLower(roleDef+"|"+v.Properties.Scope)] = v.Properties.EndDateTime
	}
	return out
}
