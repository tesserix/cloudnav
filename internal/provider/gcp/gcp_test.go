package gcp

import (
	"testing"

	"github.com/tesserix/cloudnav/internal/provider"
)

func TestParseProjects(t *testing.T) {
	data := []byte(`[
      {"projectId":"acme-prod-1","name":"Acme Prod","projectNumber":"111","lifecycleState":"ACTIVE","createTime":"2025-01-01T00:00:00Z"},
      {"projectId":"acme-dev-1","name":"Acme Dev","projectNumber":"222","lifecycleState":"ACTIVE","createTime":"2025-02-01T00:00:00Z"}
    ]`)
	nodes, err := parseProjects(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("len=%d want 2", len(nodes))
	}
	if nodes[0].ID != "acme-prod-1" || nodes[0].Name != "Acme Prod" {
		t.Errorf("nodes[0]=%+v", nodes[0])
	}
	if nodes[0].Kind != provider.KindProject {
		t.Errorf("kind=%q", nodes[0].Kind)
	}
	if nodes[0].State != "ACTIVE" {
		t.Errorf("state=%q", nodes[0].State)
	}
	// createTime propagated into Meta["createdTime"] so the TUI's shortDate
	// helper renders it in the CREATED column.
	if nodes[0].Meta["createdTime"] != "2025-01-01T00:00:00Z" {
		t.Errorf("createdTime meta = %q", nodes[0].Meta["createdTime"])
	}
}

func TestParseAssets(t *testing.T) {
	data := []byte(`[
      {"name":"//compute.googleapis.com/projects/p/zones/us-central1-a/instances/web-01","assetType":"compute.googleapis.com/Instance","location":"us-central1-a","displayName":"web-01","project":"projects/p","createTime":"2025-03-10T08:00:00Z","updateTime":"2026-01-11T10:00:00Z"},
      {"name":"//storage.googleapis.com/projects/_/buckets/my-bucket","assetType":"storage.googleapis.com/Bucket","location":"us","project":"projects/p"}
    ]`)
	parent := provider.Node{ID: "p", Kind: provider.KindProject}
	nodes, err := parseAssets(data, parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("len=%d want 2", len(nodes))
	}
	if nodes[0].Name != "web-01" {
		t.Errorf("[0].Name=%q", nodes[0].Name)
	}
	if nodes[0].State != "Instance" {
		t.Errorf("[0].State (shortType)=%q", nodes[0].State)
	}
	if nodes[1].Name != "my-bucket" {
		t.Errorf("[1].Name (derived from path)=%q", nodes[1].Name)
	}
	if nodes[1].State != "Bucket" {
		t.Errorf("[1].State=%q", nodes[1].State)
	}
	if nodes[0].Meta["createdTime"] != "2025-03-10T08:00:00Z" {
		t.Errorf("[0] createdTime = %q", nodes[0].Meta["createdTime"])
	}
	if _, ok := nodes[1].Meta["createdTime"]; ok {
		t.Error("asset without createTime should not populate the meta key")
	}
}

func TestShortType(t *testing.T) {
	cases := map[string]string{
		"compute.googleapis.com/Instance":       "Instance",
		"storage.googleapis.com/Bucket":         "Bucket",
		"containerregistry.googleapis.com/Repo": "Repo",
		"plain":                                 "plain",
	}
	for in, want := range cases {
		if got := shortType(in); got != want {
			t.Errorf("shortType(%q)=%q want %q", in, got, want)
		}
	}
}

func TestPortalURL(t *testing.T) {
	g := New()
	n := provider.Node{ID: "acme-prod-1", Kind: provider.KindProject}
	got := g.PortalURL(n)
	want := "https://console.cloud.google.com/home/dashboard?project=acme-prod-1"
	if got != want {
		t.Errorf("PortalURL=%q want %q", got, want)
	}
}
