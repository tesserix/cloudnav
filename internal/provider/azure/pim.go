package azure

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tesserix/cloudnav/internal/provider"
)

// ListEligibleRoles returns PIM-eligible role assignments across every tenant
// the caller has subscription access to. The eligibility API is scoped to the
// tenant that issued the bearer token, so we ask `az account get-access-token`
// for a token per tenant and fan the query out.
func (a *Azure) ListEligibleRoles(ctx context.Context) ([]provider.PIMRole, error) {
	out, err := a.az.Run(ctx, "account", "list", "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("list subscriptions for PIM: %w", err)
	}
	var subs []subJSON
	if err := json.Unmarshal(out, &subs); err != nil {
		return nil, fmt.Errorf("parse az account list: %w", err)
	}
	tenants := map[string]bool{}
	for _, s := range subs {
		if s.TenantID != "" {
			tenants[s.TenantID] = true
		}
	}

	url := "https://management.azure.com/providers/Microsoft.Authorization/roleEligibilityScheduleInstances?api-version=2020-10-01&$filter=asTarget()"
	allRoles := []provider.PIMRole{}
	seen := map[string]bool{}
	var lastErr error

	client := &http.Client{Timeout: 30 * time.Second}

	for tid := range tenants {
		token, err := a.tenantToken(ctx, tid)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := fetchWithToken(ctx, client, url, token)
		if err != nil {
			lastErr = err
			continue
		}
		roles, perr := parsePIM(body)
		if perr != nil {
			lastErr = perr
			continue
		}
		for _, r := range roles {
			if seen[r.ID] {
				continue
			}
			seen[r.ID] = true
			if name := a.subName(subIDFromScope(r.Scope)); name != "" {
				r.ScopeName = name
			}
			allRoles = append(allRoles, r)
		}
	}
	if len(allRoles) == 0 && lastErr != nil {
		return nil, fmt.Errorf("list eligible PIM roles: %w", lastErr)
	}
	return allRoles, nil
}

func (a *Azure) tenantToken(ctx context.Context, tenantID string) (string, error) {
	out, err := a.az.Run(ctx,
		"account", "get-access-token",
		"--tenant", tenantID,
		"--resource", "https://management.azure.com/",
		"-o", "json",
	)
	if err != nil {
		return "", err
	}
	var t struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(out, &t); err != nil {
		return "", err
	}
	if t.AccessToken == "" {
		return "", fmt.Errorf("empty token for tenant %s", tenantID)
	}
	return t.AccessToken, nil
}

func fetchWithToken(ctx context.Context, client *http.Client, url, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
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
