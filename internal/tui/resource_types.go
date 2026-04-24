package tui

import "strings"

// friendlyType maps an ARM / GCP / AWS resource type string to a short
// label suitable for a narrow TYPE column. Falls back to the segment
// after the last slash when no alias is known. Keeps the column
// readable on a laptop-width terminal and matches the shorthand
// operators already use when talking about resources.
//
// The list is curated to cover the types cloudnav users hit every
// day; anything not in the map falls through to the raw suffix so
// new resource kinds are never hidden completely.
func friendlyType(t string) string {
	if t == "" {
		return ""
	}
	lower := strings.ToLower(t)
	if alias, ok := typeAliases[lower]; ok {
		return alias
	}
	// Fall back to the segment after the last /, i.e. plain short type.
	if i := strings.LastIndex(t, "/"); i >= 0 {
		return t[i+1:]
	}
	return t
}

// typeAliases lists the short labels we prefer to show for common
// resource types. Keys are lowercased full ARM/GCP/AWS type strings.
var typeAliases = map[string]string{
	// Azure — Compute
	"microsoft.compute/virtualmachines":         "vm",
	"microsoft.compute/virtualmachinescalesets": "vmss",
	"microsoft.compute/disks":                   "disk",
	"microsoft.compute/snapshots":               "snap",
	"microsoft.compute/availabilitysets":        "avset",
	"microsoft.compute/images":                  "image",
	"microsoft.compute/diskencryptionsets":      "desk",

	// Azure — Container
	"microsoft.containerservice/managedclusters":       "aks",
	"microsoft.containerregistry/registries":           "acr",
	"microsoft.containerinstance/containergroups":      "aci",
	"microsoft.app/containerapps":                      "app",
	"microsoft.app/managedenvironments":                "appenv",

	// Azure — Network
	"microsoft.network/virtualnetworks":             "vnet",
	"microsoft.network/networkinterfaces":           "nic",
	"microsoft.network/networksecuritygroups":       "nsg",
	"microsoft.network/publicipaddresses":           "pip",
	"microsoft.network/loadbalancers":               "lb",
	"microsoft.network/applicationgateways":         "agw",
	"microsoft.network/azurefirewalls":              "afw",
	"microsoft.network/virtualnetworkgateways":      "vng",
	"microsoft.network/privateendpoints":            "pep",
	"microsoft.network/privatednszones":             "pdnsz",
	"microsoft.network/privatednszones/virtualnetworklinks": "pdnsvl",
	"microsoft.network/bastionhosts":                "bastion",
	"microsoft.network/natgateways":                 "natgw",
	"microsoft.network/routetables":                 "rt",
	"microsoft.network/dnszones":                    "dnsz",
	"microsoft.network/frontdoors":                  "afd",
	"microsoft.cdn/profiles":                        "cdn",
	"microsoft.network/connections":                 "vpn",
	"microsoft.network/localnetworkgateways":        "lng",

	// Azure — Storage + Data
	"microsoft.storage/storageaccounts":                   "sa",
	"microsoft.keyvault/vaults":                           "kv",
	"microsoft.documentdb/databaseaccounts":               "cosmos",
	"microsoft.sql/servers":                               "sqlsrv",
	"microsoft.sql/servers/databases":                     "sqldb",
	"microsoft.sql/servers/elasticpools":                  "sqlep",
	"microsoft.dbforpostgresql/servers":                   "pgsql",
	"microsoft.dbforpostgresql/flexibleservers":           "pgsql",
	"microsoft.dbformysql/servers":                        "mysql",
	"microsoft.dbformysql/flexibleservers":                "mysql",
	"microsoft.dbformariadb/servers":                      "mariadb",
	"microsoft.cache/redis":                               "redis",
	"microsoft.datafactory/factories":                     "adf",
	"microsoft.datalakestore/accounts":                    "dls",
	"microsoft.databricks/workspaces":                     "dbricks",
	"microsoft.synapse/workspaces":                        "synapse",

	// Azure — Web / Apps / Functions
	"microsoft.web/sites":                   "app",
	"microsoft.web/sites/slots":             "slot",
	"microsoft.web/serverfarms":             "plan",
	"microsoft.web/staticsites":             "swa",
	"microsoft.appconfiguration/configurationstores": "appcs",
	"microsoft.signalrservice/signalr":      "signalr",
	"microsoft.web/certificates":            "cert",
	"microsoft.logic/workflows":             "la",
	"microsoft.apimanagement/service":       "apim",

	// Azure — Messaging
	"microsoft.eventhub/namespaces":     "eh",
	"microsoft.servicebus/namespaces":   "sb",
	"microsoft.eventgrid/topics":        "egt",
	"microsoft.eventgrid/domains":       "egd",
	"microsoft.eventgrid/eventsubscriptions": "egs",
	"microsoft.notificationhubs/namespaces": "nh",

	// Azure — Ops / Observability
	"microsoft.insights/components":          "appi",
	"microsoft.insights/actiongroups":        "ag",
	"microsoft.insights/scheduledqueryrules": "alert",
	"microsoft.insights/metricalerts":        "alert",
	"microsoft.operationalinsights/workspaces": "log",
	"microsoft.automation/automationaccounts": "aa",
	"microsoft.recoveryservices/vaults":       "rsv",
	"microsoft.servicefabric/clusters":        "sf",
	"microsoft.network/networkwatchers":       "nw",

	// Azure — Identity / security
	"microsoft.managedidentity/userassignedidentities": "mi",
	"microsoft.security/pricings":                      "defender",
	"microsoft.security/autoprovisioningsettings":      "defender",
	"microsoft.policyinsights/policystates":            "policy",
	"microsoft.authorization/locks":                    "lock",

	// GCP — Compute / Containers / Storage / Net
	"compute.googleapis.com/instance":          "vm",
	"compute.googleapis.com/disk":              "disk",
	"container.googleapis.com/cluster":         "gke",
	"storage.googleapis.com/bucket":            "bucket",
	"cloudfunctions.googleapis.com/function":   "fn",
	"run.googleapis.com/service":               "run",
	"appengine.googleapis.com/service":         "gae",
	"bigquery.googleapis.com/dataset":          "bq",
	"spanner.googleapis.com/instance":          "spanner",
	"pubsub.googleapis.com/topic":              "pubsub",
	"secretmanager.googleapis.com/secret":      "secret",

	// AWS — common ARN types (the segment after the last :)
	"ec2:instance":        "ec2",
	"ec2:volume":          "ebs",
	"ec2:security-group":  "sg",
	"ec2:vpc":             "vpc",
	"ec2:subnet":          "subnet",
	"ec2:eip":             "eip",
	"ec2:natgateway":      "natgw",
	"s3:bucket":           "s3",
	"rds:db":              "rds",
	"rds:cluster":         "auroracluster",
	"lambda:function":     "lambda",
	"iam:role":            "role",
	"iam:policy":          "policy",
	"iam:user":            "user",
	"cloudwatch:alarm":    "alarm",
	"sns:topic":           "sns",
	"sqs:queue":           "sqs",
	"dynamodb:table":      "ddb",
}
