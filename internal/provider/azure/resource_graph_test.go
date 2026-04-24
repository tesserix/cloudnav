package azure

import "testing"

func TestKQLEscape(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"normal-rg", "normal-rg"},
		{"has'quote", "has''quote"},
		{"multi'quo'te", "multi''quo''te"},
		{"", ""},
	}
	for _, c := range cases {
		if got := kqlEscape(c.in); got != c.want {
			t.Errorf("kqlEscape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTagsFromRaw(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`{"env":"prod","team":"platform"}`, "env=prod,team=platform"},
		{`{"z":"1","a":"2"}`, "a=2,z=1"}, // alphabetized
		{`{}`, ""},
		{`null`, ""},
		{``, ""},
	}
	for _, c := range cases {
		if got := tagsFromRaw([]byte(c.in)); got != c.want {
			t.Errorf("tagsFromRaw(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRGResourceToNode(t *testing.T) {
	r := rgResource{
		ID:             "/subscriptions/sub1/resourceGroups/rg-a/providers/Microsoft.Compute/virtualMachines/vm1",
		Name:           "vm1",
		Type:           "microsoft.compute/virtualmachines",
		Location:       "uksouth",
		ResourceGroup:  "rg-a",
		SubscriptionID: "sub1",
		CreatedTime:    "2026-01-15T10:00:00Z",
	}
	n := rgResourceToNode(r)
	if n.ID != r.ID || n.Name != "vm1" || n.Location != "uksouth" {
		t.Errorf("identity mapping wrong: %+v", n)
	}
	if n.Meta["subscriptionId"] != "sub1" {
		t.Errorf("subscription meta missing: %+v", n.Meta)
	}
	if n.Meta["createdTime"] != "2026-01-15T10:00:00Z" {
		t.Errorf("createdTime meta missing: %+v", n.Meta)
	}
}
