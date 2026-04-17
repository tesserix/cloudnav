package azure

import "testing"

func TestParsePIMEmpty(t *testing.T) {
	roles, err := parsePIM([]byte(`{"value":[]}`))
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
	roles, err := parsePIM(payload)
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
	if _, err := parsePIM([]byte("not json")); err == nil {
		t.Error("expected error for invalid JSON")
	}
}
