package tui

import (
	"testing"

	"github.com/tesserix/cloudnav/internal/provider"
)

func node(cloud, typ string) provider.Node {
	return provider.Node{Meta: map[string]string{"type": typ, "cloud": cloud}}
}

func TestTypeColorCategory(t *testing.T) {
	cases := []struct {
		typ  string
		want string
	}{
		// Container — Azure
		{"Microsoft.ContainerService/managedClusters", catContainer},
		{"Microsoft.ContainerRegistry/registries", catContainer},
		{"Microsoft.App/containerApps", catContainer},
		// Container — GCP
		{"container.googleapis.com/Cluster", catContainer},
		{"run.googleapis.com/Service", catContainer},
		{"artifactregistry.googleapis.com/Repository", catContainer},
		// Container — AWS
		{"ecs:cluster", catContainer},
		{"eks:cluster", catContainer},
		{"ecr:repository", catContainer},
		{"apprunner:service", catContainer},
		// Compute
		{"Microsoft.Compute/virtualMachines", catCompute},
		{"compute.googleapis.com/Instance", catCompute},
		{"ec2:instance", catCompute},
		{"lambda:function", catCompute},
		// Data
		{"Microsoft.Storage/storageAccounts", catData},
		{"Microsoft.Sql/servers", catData},
		{"storage.googleapis.com/Bucket", catData},
		{"bigquery.googleapis.com/Dataset", catData},
		{"s3:bucket", catData},
		{"rds:db", catData},
		{"dynamodb:table", catData},
		// Network
		{"Microsoft.Network/virtualNetworks", catNetwork},
		{"dns.googleapis.com/ManagedZone", catNetwork},
		{"elasticloadbalancing:loadbalancer", catNetwork},
		{"cloudfront:distribution", catNetwork},
		// Security
		{"Microsoft.KeyVault/vaults", catSecurity},
		{"iam.googleapis.com/ServiceAccount", catSecurity},
		{"iam:role", catSecurity},
		{"kms:key", catSecurity},
		{"wafv2:webacl", catSecurity},
	}
	for _, c := range cases {
		got := typeColorCategory(node("any", c.typ))
		if got != c.want {
			t.Errorf("typeColorCategory(%q) = %q, want %q", c.typ, got, c.want)
		}
	}
}

func TestCategorySortOrder(t *testing.T) {
	// compute → container → data → network → security → other
	want := []string{catCompute, catContainer, catData, catNetwork, catSecurity, catOther}
	for i, c := range want {
		if got := categorySortOrder(c); got != i {
			t.Errorf("categorySortOrder(%q) = %d, want %d", c, got, i)
		}
	}
}

func TestResourceCategoryCoversAllClouds(t *testing.T) {
	// Smoke test — none of these should fall through to "other".
	cases := []string{
		// Azure
		"Microsoft.Compute/virtualMachines",
		"Microsoft.ContainerService/managedClusters",
		"Microsoft.Storage/storageAccounts",
		"Microsoft.Network/virtualNetworks",
		"Microsoft.KeyVault/vaults",
		"Microsoft.EventHub/namespaces",
		"Microsoft.ServiceBus/namespaces",
		"Microsoft.Web/sites",
		// GCP
		"compute.googleapis.com/Instance",
		"container.googleapis.com/Cluster",
		"storage.googleapis.com/Bucket",
		"bigquery.googleapis.com/Dataset",
		"iam.googleapis.com/ServiceAccount",
		"pubsub.googleapis.com/Topic",
		"dns.googleapis.com/ManagedZone",
		// AWS
		"ec2:instance",
		"eks:cluster",
		"s3:bucket",
		"rds:db",
		"route53:hostedzone",
		"iam:role",
		"sns:topic",
		"cloudwatch:alarm",
	}
	for _, typ := range cases {
		cat := resourceCategory(node("any", typ))
		if cat == catOther {
			t.Errorf("type %q unexpectedly fell through to 'other'", typ)
		}
	}
}
