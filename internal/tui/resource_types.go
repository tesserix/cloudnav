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

	// Azure — AI / ML / Analytics
	"microsoft.cognitiveservices/accounts":              "cog",
	"microsoft.machinelearningservices/workspaces":      "aml",
	"microsoft.search/searchservices":                   "search",
	"microsoft.purview/accounts":                        "purview",
	"microsoft.batch/batchaccounts":                     "batch",
	"microsoft.hdinsight/clusters":                      "hdi",
	"microsoft.streamanalytics/streamingjobs":           "streamjob",
	"microsoft.datalakeanalytics/accounts":              "dla",
	"microsoft.eventgrid/systemtopics":                  "egsys",
	"microsoft.communication/communicationservices":     "acs",

	// Azure — Extended compute / Arc / Hybrid
	"microsoft.compute/galleries":                 "gallery",
	"microsoft.compute/galleries/images":          "galimg",
	"microsoft.compute/virtualmachines/extensions": "vmext",
	"microsoft.containerservice/managedclusters/agentpools": "nodepool",
	"microsoft.hybridcompute/machines":            "arcvm",
	"microsoft.kubernetes/connectedclusters":      "arck8s",
	"microsoft.extendedlocation/customlocations":  "arcloc",
	"microsoft.azurestackhci/clusters":            "stackhci",
	"microsoft.avs/privateclouds":                 "avs",
	"microsoft.desktopvirtualization/hostpools":   "avdhost",
	"microsoft.desktopvirtualization/workspaces":  "avdws",
	"microsoft.desktopvirtualization/applicationgroups": "avdapp",

	// Azure — Extended network
	"microsoft.network/trafficmanagerprofiles":    "tm",
	"microsoft.network/ddosprotectionplans":       "ddos",
	"microsoft.network/privatelinkservices":       "pls",
	"microsoft.network/virtualwans":               "vwan",
	"microsoft.network/virtualhubs":               "vhub",
	"microsoft.network/expressroutecircuits":      "er",
	"microsoft.network/applicationsecuritygroups": "asg",
	"microsoft.network/firewallpolicies":          "fwpol",

	// Azure — Data protection / orchestration
	"microsoft.dataprotection/backupvaults": "dpv",
	"microsoft.resources/deployments":       "depl",
	"microsoft.resources/templatespecs":     "tplspec",
	"microsoft.operationsmanagement/solutions": "solution",

	// GCP — Compute Engine
	"compute.googleapis.com/instance":           "vm",
	"compute.googleapis.com/instancegroup":      "mig",
	"compute.googleapis.com/instancegroupmanager": "mig",
	"compute.googleapis.com/instancetemplate":   "tpl",
	"compute.googleapis.com/disk":               "disk",
	"compute.googleapis.com/snapshot":           "snap",
	"compute.googleapis.com/image":              "image",
	"compute.googleapis.com/machineimage":       "mimage",
	// GCP — Network
	"compute.googleapis.com/network":            "vpc",
	"compute.googleapis.com/subnetwork":         "subnet",
	"compute.googleapis.com/firewall":           "fw",
	"compute.googleapis.com/router":             "router",
	"compute.googleapis.com/address":            "ip",
	"compute.googleapis.com/globaladdress":      "gip",
	"compute.googleapis.com/forwardingrule":     "fwdrule",
	"compute.googleapis.com/globalforwardingrule": "gfwdrule",
	"compute.googleapis.com/targetpool":         "tgtpool",
	"compute.googleapis.com/targethttpproxy":    "httpproxy",
	"compute.googleapis.com/targethttpsproxy":   "httpsproxy",
	"compute.googleapis.com/urlmap":             "urlmap",
	"compute.googleapis.com/backendservice":     "bes",
	"compute.googleapis.com/backendbucket":      "beb",
	"compute.googleapis.com/healthcheck":        "hc",
	"compute.googleapis.com/sslcertificate":     "cert",
	"compute.googleapis.com/vpngateway":         "vpngw",
	"compute.googleapis.com/vpntunnel":          "vpn",
	"compute.googleapis.com/interconnect":       "ic",
	"compute.googleapis.com/interconnectattachment": "vlan",
	// GCP — Containers / Serverless
	"container.googleapis.com/cluster":          "gke",
	"container.googleapis.com/nodepool":         "nodepool",
	"run.googleapis.com/service":                "run",
	"run.googleapis.com/revision":               "rev",
	"cloudfunctions.googleapis.com/function":    "fn",
	"cloudfunctions.googleapis.com/cloudfunction": "fn",
	"appengine.googleapis.com/application":      "gae",
	"appengine.googleapis.com/service":          "gae",
	"appengine.googleapis.com/version":          "ver",
	"composer.googleapis.com/environment":       "composer",
	"dataflow.googleapis.com/job":               "dataflow",
	"dataproc.googleapis.com/cluster":           "dataproc",
	// GCP — Storage / Data
	"storage.googleapis.com/bucket":             "bucket",
	"bigquery.googleapis.com/dataset":           "bq",
	"bigquery.googleapis.com/table":             "bqtable",
	"spanner.googleapis.com/instance":           "spanner",
	"spanner.googleapis.com/database":           "spandb",
	"firestore.googleapis.com/database":         "firestore",
	"datastore.googleapis.com/entity":           "datastore",
	"bigtableadmin.googleapis.com/instance":     "bigtable",
	"bigtableadmin.googleapis.com/cluster":      "btcluster",
	"redis.googleapis.com/instance":             "redis",
	"memcache.googleapis.com/instance":          "memcache",
	"sqladmin.googleapis.com/instance":          "cloudsql",
	"sqladmin.googleapis.com/database":          "cloudsqldb",
	"filestore.googleapis.com/instance":         "filestore",
	// GCP — IAM / Security
	"iam.googleapis.com/serviceaccount":         "sa",
	"iam.googleapis.com/role":                   "role",
	"kms.googleapis.com/cryptokey":              "key",
	"kms.googleapis.com/keyring":                "keyring",
	"secretmanager.googleapis.com/secret":       "secret",
	"cloudresourcemanager.googleapis.com/project": "project",
	"cloudresourcemanager.googleapis.com/folder":  "folder",
	// GCP — Messaging / Observability
	"pubsub.googleapis.com/topic":               "pubsub",
	"pubsub.googleapis.com/subscription":        "psub",
	"cloudtasks.googleapis.com/queue":           "tasks",
	"cloudscheduler.googleapis.com/job":         "scheduler",
	"monitoring.googleapis.com/alertpolicy":     "alert",
	"monitoring.googleapis.com/uptimecheckconfig": "uptime",
	"logging.googleapis.com/logmetric":          "logmetric",
	"logging.googleapis.com/sink":               "logsink",
	// GCP — CI/CD / Artifact Registry / Build
	"artifactregistry.googleapis.com/repository": "ar",
	"artifactregistry.googleapis.com/dockerimage": "image",
	"cloudbuild.googleapis.com/build":             "build",
	"cloudbuild.googleapis.com/trigger":           "trigger",
	"sourcerepo.googleapis.com/repository":        "gitrepo",
	"containeranalysis.googleapis.com/occurrence": "vuln",
	"binaryauthorization.googleapis.com/policy":   "binauthz",
	// GCP — Service Directory / VPC Access / Private
	"vpcaccess.googleapis.com/connector":     "vpcconn",
	"servicedirectory.googleapis.com/namespace": "sd",
	"servicedirectory.googleapis.com/service":   "sdsvc",
	"privateca.googleapis.com/certificateauthority": "pca",
	"certificatemanager.googleapis.com/certificate": "cmgr",
	"beyondcorp.googleapis.com/appconnector":        "bc",
	"iap.googleapis.com/identityawareproxyclient":   "iap",
	// GCP — AI / ML / Big data
	"aiplatform.googleapis.com/endpoint":   "vertex",
	"aiplatform.googleapis.com/model":      "model",
	"aiplatform.googleapis.com/batchprediction": "pred",
	"notebooks.googleapis.com/instance":    "notebook",
	"dataplex.googleapis.com/lake":         "lake",
	"dataplex.googleapis.com/zone":         "zone",
	// GCP — Billing / Admin
	"billingbudgets.googleapis.com/budget":         "budget",
	"cloudasset.googleapis.com/feed":               "asset",
	"osconfig.googleapis.com/patchdeployment":      "osconfig",

	// AWS — Compute (Meta["type"] is "service:resourceType")
	"ec2:instance":                "ec2",
	"ec2:volume":                  "ebs",
	"ec2:snapshot":                "snap",
	"ec2:image":                   "ami",
	"ec2:launch-template":         "lt",
	"ec2:spot-instance-request":   "spot",
	"ec2:reserved-instances":      "ri",
	"ec2:placement-group":         "pg",
	"autoscaling:autoscalinggroup": "asg",
	// AWS — Network
	"ec2:security-group":          "sg",
	"ec2:vpc":                     "vpc",
	"ec2:subnet":                  "subnet",
	"ec2:route-table":             "rt",
	"ec2:internet-gateway":        "igw",
	"ec2:nat-gateway":             "natgw",
	"ec2:vpn-gateway":             "vgw",
	"ec2:vpn-connection":          "vpn",
	"ec2:customer-gateway":        "cgw",
	"ec2:transit-gateway":         "tgw",
	"ec2:eip":                     "eip",
	"ec2:network-interface":       "eni",
	"ec2:network-acl":             "nacl",
	"ec2:flow-log":                "flowlog",
	"ec2:vpc-endpoint":            "vpce",
	"elasticloadbalancing:loadbalancer": "elb",
	"route53:hostedzone":          "dns",
	"route53:record":              "dnsrec",
	"cloudfront:distribution":     "cdn",
	"apigateway:restapi":          "apigw",
	"apigateway:api":              "apigw",
	"apigatewayv2:api":            "apigwv2",
	// AWS — Containers / Serverless
	"ecs:cluster":                 "ecs",
	"ecs:service":                 "ecssvc",
	"ecs:task-definition":         "ecstask",
	"ecr:repository":              "ecr",
	"eks:cluster":                 "eks",
	"eks:nodegroup":               "eksng",
	"lambda:function":             "lambda",
	"lambda:layer":                "layer",
	"elasticbeanstalk:environment": "ebenv",
	"apprunner:service":           "apprun",
	"batch:compute-environment":   "batch",
	"batch:job-queue":             "batchq",
	// AWS — Storage / Data
	"s3:bucket":                   "s3",
	"rds:db":                      "rds",
	"rds:cluster":                 "aurora",
	"rds:snapshot":                "rdssnap",
	"rds:db-cluster-snapshot":     "aurorasnap",
	"dynamodb:table":              "ddb",
	"dynamodb:global-table":       "ddbgt",
	"dax:cluster":                 "dax",
	"elasticache:cluster":         "ec",
	"elasticache:replication-group": "ecrepl",
	"elasticache:snapshot":        "ecsnap",
	"redshift:cluster":            "redshift",
	"opensearch:domain":           "opensearch",
	"es:domain":                   "opensearch",
	"kinesis:stream":              "kinesis",
	"firehose:deliverystream":     "firehose",
	"efs:filesystem":              "efs",
	"fsx:filesystem":              "fsx",
	"backup:backup-vault":         "backup",
	"glacier:vault":               "glacier",
	"timestream:database":         "tsdb",
	// AWS — IAM / Security
	"iam:role":                    "role",
	"iam:policy":                  "policy",
	"iam:user":                    "user",
	"iam:group":                   "group",
	"iam:instance-profile":        "ip",
	"iam:oidc-provider":           "oidc",
	"kms:key":                     "kms",
	"secretsmanager:secret":       "secret",
	"ssm:parameter":               "param",
	"wafv2:webacl":                "waf",
	"wafv2:rulegroup":             "wafrules",
	"acm:certificate":             "cert",
	"cognito-idp:userpool":        "cognito",
	// AWS — Messaging / Observability / Ops
	"sns:topic":                   "sns",
	"sqs:queue":                   "sqs",
	"events:rule":                 "eb",
	"events:event-bus":            "ebus",
	"stepfunctions:statemachine":  "sfn",
	"stepfunctions:activity":      "sfnact",
	"cloudwatch:alarm":            "alarm",
	"cloudwatch:dashboard":        "dashboard",
	"logs:log-group":              "log",
	"cloudformation:stack":        "cfn",
	"cloudformation:stackset":     "cfnset",
	"cloudtrail:trail":            "ctrail",
	"config:configrule":           "configrule",
	"ssm:automation-execution":    "ssmauto",
	"ssm:document":                "ssmdoc",
	"xray:trace":                  "xray",

	// AWS — DevOps / CI-CD
	"codebuild:project":       "build",
	"codecommit:repository":   "gitrepo",
	"codedeploy:application":  "deploy",
	"codepipeline:pipeline":   "pipeline",
	"codestar:project":        "codestar",

	// AWS — ML / Analytics
	"sagemaker:notebook-instance": "smnote",
	"sagemaker:endpoint":          "smep",
	"sagemaker:model":              "smmodel",
	"sagemaker:training-job":       "smjob",
	"glue:job":                     "glue",
	"glue:crawler":                 "crawler",
	"athena:workgroup":             "athena",
	"quicksight:analysis":          "qs",
	"quicksight:dashboard":         "qsdash",
	"rekognition:collection":       "rek",
	"comprehend:document-classifier": "comprehend",
	"translate:terminology":        "translate",

	// AWS — Integration / Streaming
	"msk:cluster":                "kafka",
	"mq:broker":                  "mq",
	"appsync:graphqlapi":         "appsync",
	"appmesh:mesh":               "mesh",
	"appflow:flow":               "appflow",
	"globalaccelerator:accelerator": "ga",
	"directconnect:connection":   "dx",
	"amplify:app":                "amplify",

	// AWS — Transfer / File / Edge
	"transfer:server":   "sftp",
	"datasync:task":     "datasync",
	"mediaconvert:job":  "mconvert",
	"medialive:channel": "mlive",

	// AWS — IoT / Connect / WorkSpaces / Org
	"iot:thing":                "iot",
	"iot:policy":               "iotpol",
	"iotanalytics:pipeline":    "iotpipe",
	"connect:instance":         "connect",
	"workspaces:workspace":     "ws",
	"organizations:account":    "orgacct",
	"organizations:ou":         "orgou",
	"servicecatalog:product":   "catalog",
}
