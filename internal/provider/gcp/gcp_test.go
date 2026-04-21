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

func TestParseAssetsLabels(t *testing.T) {
	// Labels surface in Meta["tags"] as a sorted "k=v, k=v" string so the
	// TUI's TAGS column reads like it does for Azure and AWS.
	data := []byte(`[
      {"name":"//compute.googleapis.com/projects/p/zones/z/instances/vm1","assetType":"compute.googleapis.com/Instance","displayName":"vm1","labels":{"env":"prod","team":"platform","cost-center":"RD"}}
    ]`)
	parent := provider.Node{ID: "p", Kind: provider.KindProject}
	nodes, err := parseAssets(data, parent)
	if err != nil {
		t.Fatal(err)
	}
	if got := nodes[0].Meta["tags"]; got != "cost-center=RD, env=prod, team=platform" {
		t.Errorf("Meta[tags] = %q", got)
	}
}

func TestFormatGCPLabels(t *testing.T) {
	got := formatGCPLabels(map[string]string{"env": "prod", "owner": "platform"})
	if got != "env=prod, owner=platform" {
		t.Errorf("got %q", got)
	}
	if got := formatGCPLabels(nil); got != "" {
		t.Errorf("nil = %q", got)
	}
	if got := formatGCPLabels(map[string]string{"lonely": ""}); got != "lonely" {
		t.Errorf("valueless = %q", got)
	}
}

func TestParseGCPBudgetsPicksLargest(t *testing.T) {
	data := []byte(`[
		{"displayName":"monthly-small","amount":{"specifiedAmount":{"units":"500","currencyCode":"USD"}}},
		{"displayName":"monthly-large","amount":{"specifiedAmount":{"units":"10000","currencyCode":"USD"}}}
	]`)
	amount, currency, note := parseGCPBudgets(data)
	if amount != 10000 {
		t.Errorf("amount = %v, want 10000", amount)
	}
	if currency != "USD" {
		t.Errorf("currency = %q", currency)
	}
	if note == "" {
		t.Error("note should mention multiple budgets")
	}
}

func TestParseGCPBudgetsEmpty(t *testing.T) {
	if amount, _, _ := parseGCPBudgets([]byte(`[]`)); amount != 0 {
		t.Errorf("empty list should yield zero, got %v", amount)
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

func TestIsInvalidAssetType(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"INVALID_ARGUMENT: No supported asset type matches: sqladmin.googleapis.com/Database", true},
		{"INVALID_ARGUMENT: asset type is unknown", true},
		{"PERMISSION_DENIED: caller does not have permission", false},
		{"network timeout", false},
		{"", false},
	}
	for _, c := range cases {
		var err error
		if c.msg != "" {
			err = errFromString(c.msg)
		}
		if got := isInvalidAssetType(err); got != c.want {
			t.Errorf("isInvalidAssetType(%q) = %v, want %v", c.msg, got, c.want)
		}
	}
}

type testErr string

func (e testErr) Error() string { return string(e) }

func errFromString(s string) error { return testErr(s) }

func TestPortalURL(t *testing.T) {
	g := New()
	n := provider.Node{ID: "acme-prod-1", Kind: provider.KindProject}
	got := g.PortalURL(n)
	want := "https://console.cloud.google.com/home/dashboard?project=acme-prod-1"
	if got != want {
		t.Errorf("PortalURL=%q want %q", got, want)
	}
}
