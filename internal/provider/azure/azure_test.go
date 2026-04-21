package azure

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tesserix/cloudnav/internal/provider"
)

func TestShortType(t *testing.T) {
	cases := map[string]string{
		"Microsoft.Compute/virtualMachines":         "virtualMachines",
		"Microsoft.Storage/storageAccounts":         "storageAccounts",
		"Microsoft.Network/virtualNetworks/subnets": "subnets",
		"plain": "plain",
		"":      "",
	}
	for in, want := range cases {
		if got := shortType(in); got != want {
			t.Errorf("shortType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPortalURL(t *testing.T) {
	a := New()
	n := provider.Node{
		ID:   "/subscriptions/abc/resourceGroups/rg1",
		Kind: provider.KindResourceGroup,
		Meta: map[string]string{"tenantId": "tenant-1"},
	}
	want := "https://portal.azure.com/#@tenant-1/resource/subscriptions/abc/resourceGroups/rg1"
	if got := a.PortalURL(n); got != want {
		t.Errorf("PortalURL = %q, want %q", got, want)
	}
}

func TestPortalURLNoTenant(t *testing.T) {
	a := New()
	n := provider.Node{
		ID:   "/subscriptions/abc",
		Kind: provider.KindSubscription,
	}
	want := "https://portal.azure.com/#/resource/subscriptions/abc"
	if got := a.PortalURL(n); got != want {
		t.Errorf("PortalURL without tenant = %q, want %q", got, want)
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join("..", "..", "..", "test", "fixtures", "azure", name)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestParseSubs(t *testing.T) {
	nodes, err := parseSubs(fixture(t, "account_list.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(nodes))
	}
	first := nodes[0]
	if first.Name != "Acme-Prod" {
		t.Errorf("Name = %q, want Acme-Prod", first.Name)
	}
	if first.Kind != provider.KindSubscription {
		t.Errorf("Kind = %q, want subscription", first.Kind)
	}
	if first.State != "Enabled" {
		t.Errorf("State = %q, want Enabled", first.State)
	}
	if first.Meta["tenantId"] != "00000000-0000-0000-0000-000000000001" {
		t.Errorf("tenantId = %q", first.Meta["tenantId"])
	}
	if first.Meta["user"] != "alice@example.com" {
		t.Errorf("user = %q", first.Meta["user"])
	}
}

func TestParseSubsEmpty(t *testing.T) {
	nodes, err := parseSubs([]byte("[]"))
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 0 {
		t.Errorf("len = %d, want 0", len(nodes))
	}
}

func TestParseSubsInvalid(t *testing.T) {
	if _, err := parseSubs([]byte("not json")); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseRGs(t *testing.T) {
	sub := provider.Node{
		ID:   "00000000-0000-0000-0000-00000000aaaa",
		Kind: provider.KindSubscription,
		Meta: map[string]string{"tenantId": "00000000-0000-0000-0000-000000000001"},
	}
	nodes, err := parseRGs(fixture(t, "group_list.json"), sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(nodes))
	}
	rg := nodes[0]
	if rg.Name != "web-prod-rg" {
		t.Errorf("Name = %q, want web-prod-rg", rg.Name)
	}
	if rg.Location != "uksouth" {
		t.Errorf("Location = %q", rg.Location)
	}
	if rg.State != "Succeeded" {
		t.Errorf("State = %q", rg.State)
	}
	if rg.Meta["subscriptionId"] != sub.ID {
		t.Errorf("subscriptionId = %q", rg.Meta["subscriptionId"])
	}
	if rg.Meta["tenantId"] != sub.Meta["tenantId"] {
		t.Errorf("tenantId = %q", rg.Meta["tenantId"])
	}
}

func TestParseResources(t *testing.T) {
	rg := provider.Node{
		Name: "web-prod-rg",
		Kind: provider.KindResourceGroup,
		Meta: map[string]string{"tenantId": "00000000-0000-0000-0000-000000000001"},
	}
	subID := "00000000-0000-0000-0000-00000000aaaa"
	nodes, err := parseResources(fixture(t, "resource_list.json"), rg, subID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(nodes))
	}
	vm := nodes[0]
	if vm.Name != "api-01" {
		t.Errorf("Name = %q", vm.Name)
	}
	if vm.State != "virtualMachines" {
		t.Errorf("State (shortType) = %q, want virtualMachines", vm.State)
	}
	if vm.Meta["type"] != "Microsoft.Compute/virtualMachines" {
		t.Errorf("type = %q", vm.Meta["type"])
	}
	if vm.Meta["subscriptionId"] != subID {
		t.Errorf("subscriptionId = %q", vm.Meta["subscriptionId"])
	}
	if vm.Meta["createdTime"] != "2025-09-14T08:15:00Z" {
		t.Errorf("createdTime = %q — expected the $expand=createdTime value", vm.Meta["createdTime"])
	}
	if vm.Meta["changedTime"] != "2026-03-01T11:42:00Z" {
		t.Errorf("changedTime = %q", vm.Meta["changedTime"])
	}
	// Second resource has no createdTime — the field should be absent, not
	// emit an empty-string entry.
	if _, ok := nodes[1].Meta["createdTime"]; ok {
		t.Error("resource without createdTime should not set the meta key")
	}
}

func TestChildrenRejectsUnknownKind(t *testing.T) {
	a := New()
	_, err := a.Children(t.Context(), provider.Node{Kind: provider.KindTenant})
	if err == nil {
		t.Error("expected error for unsupported kind")
	}
}

func TestFormatTagsSorted(t *testing.T) {
	// Keys sort alphabetically so two identical tag maps always render the
	// same way, regardless of Go's randomised map iteration.
	got := formatTags(map[string]string{"env": "prod", "owner": "platform", "cost-center": "R&D"})
	want := "cost-center=R&D, env=prod, owner=platform"
	if got != want {
		t.Errorf("formatTags = %q, want %q", got, want)
	}
}

func TestFormatTagsEmpty(t *testing.T) {
	if got := formatTags(nil); got != "" {
		t.Errorf("nil tags = %q, want empty", got)
	}
	if got := formatTags(map[string]string{}); got != "" {
		t.Errorf("empty tags = %q, want empty", got)
	}
}

func TestFormatTagsValuelessKey(t *testing.T) {
	// Keys with empty values render as just the key (no trailing '=') so
	// we don't emit "mykey=" in the column.
	got := formatTags(map[string]string{"mykey": ""})
	if got != "mykey" {
		t.Errorf("valueless tag = %q, want %q", got, "mykey")
	}
}
