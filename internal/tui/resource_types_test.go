package tui

import "testing"

func TestFriendlyTypeAzure(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Microsoft.Compute/virtualMachines", "vm"},
		{"Microsoft.ContainerService/managedClusters", "aks"},
		{"Microsoft.Network/virtualNetworks", "vnet"},
		{"Microsoft.Network/networkSecurityGroups", "nsg"},
		{"Microsoft.Network/privateEndpoints", "pep"},
		{"Microsoft.Storage/storageAccounts", "sa"},
		{"Microsoft.KeyVault/vaults", "kv"},
		{"Microsoft.Sql/servers", "sqlsrv"},
		{"Microsoft.Cache/Redis", "redis"},
		{"Microsoft.DBforPostgreSQL/flexibleServers", "pgsql"},
	}
	for _, c := range cases {
		if got := friendlyType(c.in); got != c.want {
			t.Errorf("friendlyType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFriendlyTypeGCP(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"compute.googleapis.com/Instance", "vm"},
		{"compute.googleapis.com/Disk", "disk"},
		{"container.googleapis.com/Cluster", "gke"},
		{"storage.googleapis.com/Bucket", "bucket"},
		{"sqladmin.googleapis.com/Instance", "cloudsql"},
		{"run.googleapis.com/Service", "run"},
		{"pubsub.googleapis.com/Topic", "pubsub"},
		{"iam.googleapis.com/ServiceAccount", "sa"},
	}
	for _, c := range cases {
		if got := friendlyType(c.in); got != c.want {
			t.Errorf("friendlyType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFriendlyTypeAWS(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"ec2:instance", "ec2"},
		{"ec2:volume", "ebs"},
		{"ec2:vpc", "vpc"},
		{"s3:bucket", "s3"},
		{"rds:db", "rds"},
		{"rds:cluster", "aurora"},
		{"lambda:function", "lambda"},
		{"eks:cluster", "eks"},
		{"cloudfront:distribution", "cdn"},
		{"route53:hostedzone", "dns"},
		{"iam:role", "role"},
	}
	for _, c := range cases {
		if got := friendlyType(c.in); got != c.want {
			t.Errorf("friendlyType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFriendlyTypeUnknownFallsBack(t *testing.T) {
	// Unknown types fall back to the segment after the last slash
	// so new resource kinds aren't hidden.
	cases := []struct {
		in, want string
	}{
		{"Microsoft.SomeNewService/someResource", "someResource"},
		{"somecloud.example/brandNewThing", "brandNewThing"},
		{"plainString", "plainString"},
		{"", ""},
	}
	for _, c := range cases {
		if got := friendlyType(c.in); got != c.want {
			t.Errorf("friendlyType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
