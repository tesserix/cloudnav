package gcp

import (
	"errors"
	"testing"

	"github.com/tesserix/cloudnav/internal/provider"
)

func TestParsePAMEntitlements(t *testing.T) {
	body := []byte(`[
      {
        "name":"projects/demo/locations/global/entitlements/break-glass-owner",
        "maxRequestDuration":"28800s",
        "privilegedAccess":{
          "gcpIamAccess":{
            "resource":"//cloudresourcemanager.googleapis.com/projects/demo",
            "roleBindings":[{"role":"roles/owner"}]
          }
        }
      },
      {
        "name":"projects/demo/locations/global/entitlements/sql-admin",
        "maxRequestDuration":"3600s",
        "privilegedAccess":{
          "gcpIamAccess":{
            "resource":"//cloudresourcemanager.googleapis.com/projects/demo",
            "roleBindings":[{"role":"roles/cloudsql.admin"}]
          }
        }
      }
    ]`)
	roles, err := parsePAMEntitlements(body, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 2 {
		t.Fatalf("len=%d want 2", len(roles))
	}
	if roles[0].RoleName != "roles/owner" {
		t.Errorf("[0].RoleName=%q", roles[0].RoleName)
	}
	if roles[0].ScopeName != "demo" {
		t.Errorf("[0].ScopeName=%q", roles[0].ScopeName)
	}
	if roles[0].Source != "gcp-pam" {
		t.Errorf("[0].Source=%q", roles[0].Source)
	}
	if roles[0].MaxDurationHours != 8 {
		t.Errorf("[0].MaxDurationHours=%d want 8 (from 28800s)", roles[0].MaxDurationHours)
	}
	if roles[1].MaxDurationHours != 1 {
		t.Errorf("[1].MaxDurationHours=%d want 1 (from 3600s)", roles[1].MaxDurationHours)
	}
}

func TestParsePAMActiveGrants(t *testing.T) {
	body := []byte(`[
      {"name":"projects/demo/locations/global/entitlements/break-glass-owner/grants/g1","state":"ACTIVE","expireTime":"2027-01-01T00:00:00Z"},
      {"name":"projects/demo/locations/global/entitlements/break-glass-owner/grants/g0","state":"APPROVED","expireTime":"2099-01-01T00:00:00Z"},
      {"name":"projects/demo/locations/global/entitlements/sql-admin/grants/g2","state":"ACTIVE","expireTime":"2027-02-01T00:00:00Z","entitlement":"projects/demo/locations/global/entitlements/sql-admin"}
    ]`)
	active := parsePAMActiveGrants(body)
	if active["projects/demo/locations/global/entitlements/break-glass-owner"] != "2027-01-01T00:00:00Z" {
		t.Errorf("bg-owner not mapped: %v", active)
	}
	if active["projects/demo/locations/global/entitlements/sql-admin"] != "2027-02-01T00:00:00Z" {
		t.Errorf("sql-admin (explicit entitlement field) not mapped: %v", active)
	}
	// APPROVED (not ACTIVE) must be ignored.
	if len(active) != 2 {
		t.Errorf("expected 2 active grants, got %d: %v", len(active), active)
	}
}

func TestParsePAMDurationHours(t *testing.T) {
	cases := map[string]int{
		"":        0,
		"3600s":   1,
		"28800s":  8,
		"3601s":   2, // rounds up
		"garbage": 0,
	}
	for in, want := range cases {
		if got := parsePAMDurationHours(in); got != want {
			t.Errorf("parsePAMDurationHours(%q)=%d want %d", in, got, want)
		}
	}
}

func TestIsPAMNotEnabled(t *testing.T) {
	if !isPAMNotEnabled(errors.New("privilegedaccessmanager.googleapis.com has not been used in project 123 before or it is disabled")) {
		t.Error("expected PAM-disabled detection")
	}
	if isPAMNotEnabled(errors.New("network timeout")) {
		t.Error("false positive on unrelated error")
	}
	if isPAMNotEnabled(nil) {
		t.Error("nil should be false")
	}
}

func TestGCPSatisfiesPIMer(t *testing.T) {
	var _ provider.PIMer = (*GCP)(nil)
}
