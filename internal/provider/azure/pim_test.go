package azure

import "testing"

func TestParsePIMEmpty(t *testing.T) {
	roles, err := parseAzurePIM([]byte(`{"value":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 0 {
		t.Errorf("len = %d, want 0", len(roles))
	}
}

func TestParsePIM(t *testing.T) {
	payload := []byte(`{"value":[
      {
        "id":"/providers/Microsoft.Authorization/roleEligibilityScheduleInstances/aaaa",
        "properties":{
          "scope":"/subscriptions/abc",
          "principalId":"user-1",
          "endDateTime":"2027-01-01T00:00:00Z",
          "expandedProperties":{"roleDefinition":{"displayName":"Reader"}}
        }
      },
      {
        "id":"/providers/Microsoft.Authorization/roleEligibilityScheduleInstances/bbbb",
        "properties":{
          "scope":"/subscriptions/xyz/resourceGroups/rg1",
          "principalId":"user-1",
          "expandedProperties":{"roleDefinition":{"displayName":"Cost Management Reader"}}
        }
      }
    ]}`)
	roles, err := parseAzurePIM(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 2 {
		t.Fatalf("len = %d, want 2", len(roles))
	}
	if roles[0].RoleName != "Reader" {
		t.Errorf("[0].RoleName = %q", roles[0].RoleName)
	}
	if roles[0].Scope != "/subscriptions/abc" {
		t.Errorf("[0].Scope = %q", roles[0].Scope)
	}
	if roles[0].EndDateTime != "2027-01-01T00:00:00Z" {
		t.Errorf("[0].EndDateTime = %q", roles[0].EndDateTime)
	}
	if roles[1].RoleName != "Cost Management Reader" {
		t.Errorf("[1].RoleName = %q", roles[1].RoleName)
	}
}

func TestParsePIMBadJSON(t *testing.T) {
	if _, err := parseAzurePIM([]byte("not json")); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseISO8601Hours(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"PT8H", 8},
		{"PT1H", 1},
		{"PT30M", 1},
		{"PT1H30M", 2},
		{"P1D", 24},
		{"P1DT2H", 26},
		{"", 0},
		{"garbage", 0},
	}
	for _, c := range cases {
		if got := parseISO8601Hours(c.in); got != c.want {
			t.Errorf("parseISO8601Hours(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestMaxHoursFromRules(t *testing.T) {
	body := []byte(`{"properties":{"rules":[
      {"id":"Expiration_EndUser_Assignment","ruleType":"RoleManagementPolicyExpirationRule","maximumDuration":"PT8H"},
      {"id":"Approval_EndUser_Assignment","ruleType":"RoleManagementPolicyApprovalRule"}
    ]}}`)
	if got := maxHoursFromRules(body); got != 8 {
		t.Errorf("maxHoursFromRules = %d, want 8", got)
	}
	if got := maxHoursFromRules([]byte(`{}`)); got != 0 {
		t.Errorf("empty policy = %d, want 0", got)
	}
}

func TestParseActiveAssignments(t *testing.T) {
	body := []byte(`{"value":[{"properties":{
      "scope":"/subscriptions/abc",
      "roleDefinitionId":"/providers/Microsoft.Authorization/roleDefinitions/111",
      "endDateTime":"2030-01-01T00:00:00Z"
    }}]}`)
	m := parseActiveAssignments(body)
	if len(m) != 1 {
		t.Fatalf("len = %d, want 1", len(m))
	}
	key := "/providers/microsoft.authorization/roledefinitions/111|/subscriptions/abc"
	if m[key] != "2030-01-01T00:00:00Z" {
		t.Errorf("endDateTime = %q", m[key])
	}
}
