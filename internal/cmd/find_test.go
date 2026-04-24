package cmd

import (
	"context"
	"fmt"
	"testing"

	"github.com/tesserix/cloudnav/internal/provider"
)

type fakeProvider struct {
	name     string
	roots    []provider.Node
	children map[string][]provider.Node
	details  map[string][]byte
	roles    []provider.PIMRole
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) LoggedIn(context.Context) error { return nil }

func (f *fakeProvider) Root(context.Context) ([]provider.Node, error) {
	return cloneFakeNodes(f.roots), nil
}

func (f *fakeProvider) Children(_ context.Context, parent provider.Node) ([]provider.Node, error) {
	return cloneFakeNodes(f.children[f.nodeKey(parent)]), nil
}

func (f *fakeProvider) PortalURL(n provider.Node) string {
	return "https://example.test/" + n.ID
}

func (f *fakeProvider) Details(_ context.Context, n provider.Node) ([]byte, error) {
	if body, ok := f.details[f.nodeKey(n)]; ok {
		return body, nil
	}
	return nil, fmt.Errorf("no details for %s", f.nodeKey(n))
}

func (f *fakeProvider) ListEligibleRoles(context.Context) ([]provider.PIMRole, error) {
	out := make([]provider.PIMRole, len(f.roles))
	copy(out, f.roles)
	return out, nil
}

func (f *fakeProvider) ActivateRole(context.Context, provider.PIMRole, string, int) error {
	return nil
}

func (f *fakeProvider) nodeKey(n provider.Node) string {
	return fmt.Sprintf("%s:%s", n.Kind, n.ID)
}

func cloneFakeNodes(in []provider.Node) []provider.Node {
	if in == nil {
		return nil
	}
	out := make([]provider.Node, len(in))
	copy(out, in)
	return out
}

func TestCollectScopeMatchesWalksScopesButNotResources(t *testing.T) {
	folder := provider.Node{ID: "folders/100", Name: "Finance", Kind: provider.KindFolder}
	project := provider.Node{ID: "acme-billing", Name: "Billing Project", Kind: provider.KindProject, Parent: &folder}
	resource := provider.Node{
		ID:     "//storage.googleapis.com/projects/_/buckets/finance-data",
		Name:   "finance-data",
		Kind:   provider.KindResource,
		Parent: &project,
	}
	p := &fakeProvider{
		name:  cloudGCP,
		roots: []provider.Node{folder},
		children: map[string][]provider.Node{
			"folder:folders/100":   {project},
			"project:acme-billing": {resource},
		},
	}

	got, err := collectScopeMatches(t.Context(), p, "billing", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Kind != "project" || got[0].Name != "Billing Project" {
		t.Fatalf("match = %+v", got[0])
	}

	got, err = collectScopeMatches(t.Context(), p, "finance-data", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("resource should not appear in scope search, got %+v", got)
	}
}

func TestCollectResourceMatchesHonorsAzureSubscriptionScope(t *testing.T) {
	sub1 := provider.Node{ID: "sub-1", Name: "Prod", Kind: provider.KindSubscription}
	sub2 := provider.Node{ID: "sub-2", Name: "Dev", Kind: provider.KindSubscription}
	rg1 := provider.Node{
		ID:     "/subscriptions/sub-1/resourceGroups/prod-rg",
		Name:   "prod-rg",
		Kind:   provider.KindResourceGroup,
		Parent: &sub1,
		Meta:   map[string]string{"subscriptionId": "sub-1"},
	}
	rg2 := provider.Node{
		ID:     "/subscriptions/sub-2/resourceGroups/dev-rg",
		Name:   "dev-rg",
		Kind:   provider.KindResourceGroup,
		Parent: &sub2,
		Meta:   map[string]string{"subscriptionId": "sub-2"},
	}
	res1 := provider.Node{
		ID:       "/subscriptions/sub-1/resourceGroups/prod-rg/providers/Microsoft.Compute/virtualMachines/app-01",
		Name:     "app-01",
		Kind:     provider.KindResource,
		Location: "uksouth",
		State:    "virtualMachines",
		Parent:   &rg1,
		Meta: map[string]string{
			"type":           "Microsoft.Compute/virtualMachines",
			"subscriptionId": "sub-1",
		},
	}
	res2 := provider.Node{
		ID:       "/subscriptions/sub-2/resourceGroups/dev-rg/providers/Microsoft.Compute/virtualMachines/app-02",
		Name:     "app-02",
		Kind:     provider.KindResource,
		Location: "uksouth",
		State:    "virtualMachines",
		Parent:   &rg2,
		Meta: map[string]string{
			"type":           "Microsoft.Compute/virtualMachines",
			"subscriptionId": "sub-2",
		},
	}
	p := &fakeProvider{
		name:  cloudAzure,
		roots: []provider.Node{sub1, sub2},
		children: map[string][]provider.Node{
			"subscription:sub-1":                             {rg1},
			"subscription:sub-2":                             {rg2},
			"rg:/subscriptions/sub-1/resourceGroups/prod-rg": {res1},
			"rg:/subscriptions/sub-2/resourceGroups/dev-rg":  {res2},
		},
	}

	got, err := collectResourceMatches(t.Context(), p, "app", findOptions{Subscription: "sub-1"}, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].ID != res1.ID {
		t.Fatalf("got %q, want %q", got[0].ID, res1.ID)
	}
}

func TestNodeMatchesQueryUsesMetaAndParentFields(t *testing.T) {
	parent := provider.Node{ID: "prod-rg", Name: "prod-rg", Kind: provider.KindResourceGroup}
	node := provider.Node{
		ID:       "vm-01",
		Name:     "vm-01",
		Kind:     provider.KindResource,
		Location: "uksouth",
		Meta: map[string]string{
			"type": "Microsoft.Sql/servers",
			"tags": "team=platform",
		},
		Parent: &parent,
	}

	if !nodeMatchesQuery(node, "sql") {
		t.Fatal("expected match on meta[type]")
	}
	if !nodeMatchesQuery(node, "platform") {
		t.Fatal("expected match on meta[tags]")
	}
	if !nodeMatchesQuery(node, "prod-rg") {
		t.Fatal("expected match on parent name")
	}
	if nodeMatchesQuery(node, "redis") {
		t.Fatal("unexpected match")
	}
}

func TestCollectPIMMatchesNormalizesSourceAndScope(t *testing.T) {
	p := &fakeProvider{
		name: cloudAzure,
		roles: []provider.PIMRole{
			{
				ID:          "role-1",
				RoleName:    "Billing Reader",
				ScopeName:   "Prod Subscription",
				Source:      "",
				Active:      true,
				ActiveUntil: "2026-04-24T12:00:00Z",
			},
		},
	}

	got, err := collectPIMMatches(t.Context(), p, "billing", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Source != "azure" {
		t.Fatalf("source = %q, want azure", got[0].Source)
	}
	if got[0].Scope != "Prod Subscription" {
		t.Fatalf("scope = %q, want Prod Subscription", got[0].Scope)
	}
	if !got[0].Active {
		t.Fatal("expected active=true")
	}
}
