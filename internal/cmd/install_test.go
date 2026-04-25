package cmd

import (
	"testing"
)

func TestToolByNameZellij(t *testing.T) {
	tool, ok := toolByName("zellij")
	if !ok {
		t.Fatal("toolByName(zellij) should resolve")
	}
	if tool.Name != "zellij" || tool.Bin != "zellij" {
		t.Errorf("zellij metadata: %+v", tool)
	}
}

func TestToolByNameUnknown(t *testing.T) {
	if _, ok := toolByName("not-a-tool"); ok {
		t.Error("toolByName should miss on unknown name")
	}
}

// Cloud names must NOT resolve as tools — they go through the
// provider path. This guards against accidentally adding "azure" to
// the tool table and breaking the install dispatch.
func TestToolByNameRejectsCloudNames(t *testing.T) {
	for _, name := range []string{cloudAzure, cloudGCP, cloudAWS} {
		if _, ok := toolByName(name); ok {
			t.Errorf("toolByName(%q) should NOT resolve — cloud names belong on the provider path", name)
		}
	}
}

func TestValidInstallArgsCoversCloudsAndZellij(t *testing.T) {
	want := map[string]bool{cloudAzure: false, cloudAWS: false, cloudGCP: false, "zellij": false}
	for _, a := range validInstallArgs {
		if _, ok := want[a]; ok {
			want[a] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("validInstallArgs missing %q", k)
		}
	}
}
