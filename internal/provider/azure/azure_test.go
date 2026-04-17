package azure

import (
	"testing"

	"github.com/tesserix/cloudnav/internal/provider"
)

func TestShortType(t *testing.T) {
	cases := map[string]string{
		"Microsoft.Compute/virtualMachines":         "virtualMachines",
		"Microsoft.Storage/storageAccounts":         "storageAccounts",
		"Microsoft.Network/virtualNetworks/subnets": "subnets",
		"plain":                                     "plain",
		"":                                          "",
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
