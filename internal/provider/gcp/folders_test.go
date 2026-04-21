package gcp

import (
	"testing"

	"github.com/tesserix/cloudnav/internal/provider"
)

func TestParseFoldersTopLevelOnly(t *testing.T) {
	// Nested folders should be filtered out of the top-level view — we only
	// surface direct children of the configured org. Users drill into a
	// folder to see its descendants.
	data := []byte(`[
		{"name":"folders/100","displayName":"Engineering","parent":"organizations/456","state":"ACTIVE"},
		{"name":"folders/200","displayName":"Finance","parent":"organizations/456","state":"ACTIVE"},
		{"name":"folders/300","displayName":"nested-team","parent":"folders/100","state":"ACTIVE"}
	]`)
	nodes, err := parseFolders(data, "organizations/456")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("got %d folders, want 2 (nested should be filtered)", len(nodes))
	}
	// Alphabetical ordering.
	if nodes[0].Name != "Engineering" || nodes[1].Name != "Finance" {
		t.Errorf("order = [%s, %s]", nodes[0].Name, nodes[1].Name)
	}
	if nodes[0].Kind != provider.KindFolder {
		t.Errorf("kind = %q, want folder", nodes[0].Kind)
	}
}

func TestParseFoldersEmpty(t *testing.T) {
	nodes, err := parseFolders([]byte(`[]`), "organizations/456")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 0 {
		t.Errorf("len = %d, want 0", len(nodes))
	}
}

func TestFolderNumberFromID(t *testing.T) {
	cases := map[string]string{
		"folders/123": "123",
		"123":         "123",
		"":            "",
	}
	for in, want := range cases {
		if got := folderNumberFromID(in); got != want {
			t.Errorf("folderNumberFromID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOrgIDAcceptsBothForms(t *testing.T) {
	// Accepts the Resource Manager form and the bare id; trims the prefix
	// so downstream calls always see a plain numeric id.
	t.Setenv(envGCPOrg, "organizations/12345")
	if got := orgID(); got != "12345" {
		t.Errorf("organizations/12345 → %q, want 12345", got)
	}
	t.Setenv(envGCPOrg, "67890")
	if got := orgID(); got != "67890" {
		t.Errorf("bare id → %q, want 67890", got)
	}
	t.Setenv(envGCPOrg, "")
	if got := orgID(); got != "" {
		t.Errorf("unset → %q, want empty", got)
	}
}
