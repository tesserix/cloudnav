package azure

import "testing"

func TestParseLocks(t *testing.T) {
	data := []byte(`[
      {"id":"/subscriptions/s1/resourceGroups/rg-a","name":"keep","level":"CanNotDelete"},
      {"id":"/subscriptions/s1/resourceGroups/rg-a/providers/Microsoft.Sql/servers/x","name":"keep-sql","level":"ReadOnly"},
      {"id":"/subscriptions/s1/resourceGroups/rg-b","name":"keep-b","level":"ReadOnly"}
    ]`)
	locks, err := parseLocks(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(locks) != 2 {
		t.Fatalf("buckets=%d want 2", len(locks))
	}
	if len(locks["rg-a"]) != 2 {
		t.Errorf("rg-a=%d want 2", len(locks["rg-a"]))
	}
	if locks["rg-b"][0].Level != "ReadOnly" {
		t.Errorf("rg-b level=%q", locks["rg-b"][0].Level)
	}
}

func TestRGFromScope(t *testing.T) {
	cases := map[string]string{
		"/subscriptions/s1/resourceGroups/rg-a":                         "rg-a",
		"/subscriptions/s1/resourceGroups/rg-a/providers/Microsoft.X/y": "rg-a",
		"/providers/Microsoft.Authorization/roleDefinitions/x":          "",
		"": "",
	}
	for in, want := range cases {
		if got := rgFromScope(in); got != want {
			t.Errorf("rgFromScope(%q)=%q want %q", in, got, want)
		}
	}
}
