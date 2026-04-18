package azure

import "testing"

func TestParseEntraEligible(t *testing.T) {
	body := []byte(`{"value":[
	  {"id":"e1","principalId":"u1","roleDefinitionId":"rd1","directoryScopeId":"/",
	   "endDateTime":"2030-01-01T00:00:00Z",
	   "roleDefinition":{"displayName":"Application Developer","id":"rd1"}},
	  {"id":"e2","principalId":"u1","roleDefinitionId":"rd2","directoryScopeId":"/administrativeUnits/au-1",
	   "roleDefinition":{"displayName":"Directory Readers","id":"rd2"}}
	]}`)
	roles, err := parseEntraEligible(body, "tid-1", "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 2 {
		t.Fatalf("len = %d, want 2", len(roles))
	}
	if roles[0].Source != "entra" {
		t.Errorf("[0].Source = %q, want entra", roles[0].Source)
	}
	if roles[0].RoleName != "Application Developer" {
		t.Errorf("[0].RoleName = %q", roles[0].RoleName)
	}
	if roles[0].ScopeName != "Directory" {
		t.Errorf("[0].ScopeName = %q, want Directory", roles[0].ScopeName)
	}
	if roles[1].ScopeName != "AU au-1" {
		t.Errorf("[1].ScopeName = %q", roles[1].ScopeName)
	}
	if roles[0].TenantID != "tid-1" {
		t.Errorf("[0].TenantID = %q", roles[0].TenantID)
	}
}

func TestParseEntraActive(t *testing.T) {
	body := []byte(`{"value":[{
	  "id":"a1","principalId":"u1","roleDefinitionId":"rd1","directoryScopeId":"/",
	  "endDateTime":"2030-01-01T00:00:00Z",
	  "roleDefinition":{"displayName":"Application Developer","id":"rd1"}
	}]}`)
	m := parseEntraActive(body)
	if m["rd1|/"] != "2030-01-01T00:00:00Z" {
		t.Errorf("active map = %v", m)
	}
}

func TestParseGroupEligible(t *testing.T) {
	body := []byte(`{"value":[{
	  "id":"g1","principalId":"u1","groupId":"grp-1","accessId":"member",
	  "endDateTime":"2030-01-01T00:00:00Z",
	  "group":{"id":"grp-1","displayName":"Platform-Ops-Privileged-Group"}
	}]}`)
	roles, err := parseGroupEligible(body, "tid-1", "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 {
		t.Fatalf("len = %d, want 1", len(roles))
	}
	r := roles[0]
	if r.Source != "group" {
		t.Errorf("Source = %q", r.Source)
	}
	if r.GroupID != "grp-1" {
		t.Errorf("GroupID = %q", r.GroupID)
	}
	if r.RoleName != "member" {
		t.Errorf("RoleName = %q", r.RoleName)
	}
	if r.ScopeName != "Platform-Ops-Privileged-Group" {
		t.Errorf("ScopeName = %q", r.ScopeName)
	}
}
