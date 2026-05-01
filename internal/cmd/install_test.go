package cmd

import (
	"testing"
)

func TestValidInstallArgsCoversAllClouds(t *testing.T) {
	want := map[string]bool{cloudAzure: false, cloudAWS: false, cloudGCP: false}
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
