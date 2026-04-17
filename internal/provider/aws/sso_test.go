package aws

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadSSOProfilesEmpty(t *testing.T) {
	p := writeConfig(t, `[default]
region = us-east-1
output = json
`)
	profiles, err := loadSSOProfiles(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 0 {
		t.Errorf("len=%d want 0", len(profiles))
	}
}

func TestLoadSSOProfilesWithSSO(t *testing.T) {
	p := writeConfig(t, `[default]
region = us-east-1

[profile dev]
region = eu-west-1
sso_session = company
sso_account_id = 111111111111
sso_role_name = DeveloperAccess

[profile prod]
region = us-east-1
sso_session = company
sso_account_id = 222222222222
sso_role_name = ReadOnlyAccess

[sso-session company]
sso_start_url = https://company.awsapps.com/start
sso_region = us-east-1
`)
	profiles, err := loadSSOProfiles(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 {
		t.Fatalf("len=%d want 2", len(profiles))
	}
	if profiles[0].name != "dev" {
		t.Errorf("[0].name=%q", profiles[0].name)
	}
	if profiles[0].accountID != "111111111111" || profiles[0].roleName != "DeveloperAccess" {
		t.Errorf("[0]=%+v", profiles[0])
	}
	if profiles[1].name != "prod" {
		t.Errorf("[1].name=%q", profiles[1].name)
	}
}

func TestLoadSSOProfilesMissingFile(t *testing.T) {
	profiles, err := loadSSOProfiles("/tmp/definitely-not-here/aws-config")
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if len(profiles) != 0 {
		t.Errorf("len=%d want 0", len(profiles))
	}
}
