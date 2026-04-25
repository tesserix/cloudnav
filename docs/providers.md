# Providers

cloudnav talks to each cloud through its official Go SDK, with the
matching CLI as a fallback path. Auth flows through the standard
SDK credential chain per cloud — see [`auth.md`](auth.md) for the
full method matrix and env vars.

## Azure

**Primary path:** Azure SDK for Go (`azcore`, `azidentity`,
`armresources`, `armcompute`, `armlocks`, …).
**CLI fallback:** `az` (≥ 2.50) — used only when the SDK auth
chain can't resolve credentials.

**Auth methods accepted (resolved by `azidentity.NewDefaultAzureCredential`):**

- Workload Identity (federated token file)
- Service Principal — client secret (`AZURE_CLIENT_SECRET`)
- Service Principal — certificate (`AZURE_CLIENT_CERTIFICATE_PATH`)
- Managed Identity (Azure VMs, App Service, Functions)
- Azure CLI cached token (`az login`)

**Permissions required for read-only navigation:**

- Subscription list — any Entra ID user automatically sees the
  subscriptions they are a member of.
- Resource Group / Resource list — `Reader` on the subscription
  or RG.
- Cost column — `Cost Management Reader` on the billing scope.
- PIM listing —
  `Microsoft.Authorization/roleEligibilityScheduleInstances/read`
  (granted by default to users with eligible assignments).

**SDK + CLI surfaces wrapped:**

| Level | SDK | CLI fallback |
|-------|-----|--------------|
| Subscriptions | `armsubscription.Client` (multi-tenant) | `az account list` |
| Resource groups | Resource Graph (KQL) | `az group list` |
| Resources | Resource Graph + ARM REST | `az resource list` |
| Details | ARM REST direct | `az resource show` |
| Locks | `armlocks.ManagementLocksClient` | `az lock list` |
| VM ops | `armcompute.VirtualMachinesClient` | `az vm start/stop` |
| Cost | Cost Management REST + Resource Graph | — |
| PIM | Microsoft Graph REST | — |

## GCP

**Primary path:** Google Cloud Go SDKs
(`cloud.google.com/go/resourcemanager/apiv3`,
`asset/apiv1`, `compute/apiv1`, `recommender/apiv1`, `billing/apiv1`,
`bigquery`, `monitoring/apiv3/v2`,
`privilegedaccessmanager/apiv1`, `servicehealth/apiv1`).
**CLI fallback:** `gcloud`.

**Auth methods accepted (resolved by `google.FindDefaultCredentials` /
ADC chain):**

- Service Account JSON (`GOOGLE_APPLICATION_CREDENTIALS`)
- Workload Identity Federation (`external_account` JSON)
- Impersonated Service Account JSON
- Metadata server (GCE / Cloud Run / GKE)
- gcloud user creds (`gcloud auth application-default login`)

**SDK migration:** all 12 phases done — see
[`gcp-sdk-migration.md`](gcp-sdk-migration.md) for the full roadmap
and per-phase commit history.

**SDK + CLI surfaces wrapped:**

| Level | SDK | CLI fallback |
|-------|-----|--------------|
| Projects | `resourcemanager/apiv3.ProjectsClient` | `gcloud projects list` |
| Folders | `resourcemanager/apiv3.FoldersClient` | `gcloud resource-manager folders list` |
| Resources | `asset/apiv1.SearchAllResources` | `gcloud asset search-all-resources` |
| Details (project) | `resourcemanager/apiv3.GetProject` | `gcloud projects describe` |
| Details (resource) | `asset/apiv1.SearchAllResources` (name=) | `gcloud asset search-all-resources --query=name:` |
| VM ops | `compute/apiv1.InstancesClient` | `gcloud compute instances` |
| Advisor | `recommender/apiv1.ListRecommendations` | `gcloud recommender recommendations list` |
| Cost | `bigquery.Client` against billing-export | `gcloud alpha bq query` |
| Budgets | `billing/budgets/apiv1.ListBudgets` | `gcloud billing budgets list` |
| Liens (lock-equivalent) | — (no v3 SDK) | `gcloud alpha resource-manager liens` |
| Metrics | `monitoring/apiv3/v2.ListTimeSeries` | `gcloud monitoring time-series list` |
| PIM (PAM) | `privilegedaccessmanager/apiv1` | `gcloud beta pam entitlements list` |
| Service Health | `servicehealth/apiv1.ListEvents` | — |

## AWS

**Primary path:** AWS SDK for Go v2 (`config`, `sts`,
`organizations`, `ec2`, `resourcegroupstaggingapi`,
`costexplorer`, `budgets`, `cloudwatch`, `computeoptimizer`,
`support`, `health`, `s3`).
**CLI fallback:** `aws` v2.

**Auth methods accepted (resolved by `config.LoadDefaultConfig`):**

- Web Identity / OIDC (IRSA, GitHub Actions OIDC)
- Static IAM keys (`AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`)
- Temporary credentials (`AWS_SESSION_TOKEN`)
- Named profile (`AWS_PROFILE` → `~/.aws/config`)
- Default profile / SSO (`aws configure sso`)
- ECS task role (container metadata)
- EC2 IMDS / IMDSv2

**AssumeRole** for cross-account elevation works through profiles
(`role_arn` + `source_profile` keys); cloudnav uses whatever
temporary creds the SDK chain returns.

**SDK + CLI surfaces wrapped:**

| Level | SDK | CLI fallback |
|-------|-----|--------------|
| Accounts | `organizations.ListAccounts` (+ STS single-account fallback) | `aws organizations list-accounts` / `aws sts get-caller-identity` |
| Regions | `ec2.DescribeRegions` | `aws ec2 describe-regions` |
| Resources | `resourcegroupstaggingapi.GetResources` | `aws resourcegroupstaggingapi get-resources` |
| VM ops | `ec2.DescribeInstances` / `Start` / `Stop` / `Terminate` | `aws ec2` |
| Cost | `costexplorer.GetCostAndUsage` / `GetAnomalies` / `GetCostForecast` | `aws ce` |
| Budgets | `budgets.DescribeBudgets` | `aws budgets describe-budgets` |
| Cost history | `costexplorer.GetCostAndUsage` (daily granularity) | — |
| Metrics | `cloudwatch.GetMetricData` | `aws cloudwatch get-metric-data` |
| Advisor | `computeoptimizer` (EC2 / EBS) + Cost Anomalies | `aws compute-optimizer` |
| Trusted Advisor | — (CLI only — paid Business / Enterprise plan) | `aws support` |
| Health (incidents) | `health.DescribeEvents` | — |
| Delete (EC2 / S3) | `ec2.TerminateInstances` / `s3.DeleteBucket` | — |
| Lock | — | — (no native AWS lock primitive; SCPs are policy-shaped) |
