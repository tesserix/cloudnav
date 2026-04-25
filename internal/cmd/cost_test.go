package cmd

import (
	"strings"
	"testing"
)

func TestCostProjectsRegisteredUnderCost(t *testing.T) {
	subs := costCmd.Commands()
	want := map[string]bool{
		"subs":     false,
		"rgs":      false,
		"regions":  false,
		"services": false,
		"projects": false,
	}
	for _, c := range subs {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("cost subcommand %q not registered", name)
		}
	}
}

func TestCostProjectsFlagsDeclared(t *testing.T) {
	for _, name := range []string{"json", "match", "limit"} {
		if costProjectsCmd.Flags().Lookup(name) == nil {
			t.Errorf("cost projects --%s flag not declared", name)
		}
	}
}

func TestCostProjectsHelpMentionsBigQuery(t *testing.T) {
	if !strings.Contains(costProjectsCmd.Long, "BigQuery") {
		t.Error("cost projects help should reference BigQuery billing-export")
	}
	if !strings.Contains(costProjectsCmd.Long, "CLOUDNAV_GCP_BILLING_TABLE") {
		t.Error("cost projects help should mention the env var override")
	}
}
