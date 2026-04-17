# Providers

## Azure

**CLI:** `az` (≥ 2.50).
**Login:** `az login` (interactive) or `az login --service-principal` for CI.

**Permissions required for read-only navigation:**

- Subscription list — any Entra ID user automatically sees the subscriptions they are a member of.
- Resource Group / Resource list — `Reader` on the subscription or RG.
- Cost column — `Cost Management Reader` on the billing scope.
- PIM listing — `Microsoft.Authorization/roleEligibilityScheduleInstances/read` (granted by default to users with eligible assignments).

**CLI commands we wrap:**

| Level | Command |
|-------|---------|
| Subscriptions | `az account list` |
| Resource groups | `az group list --subscription <id>` |
| Resources | `az resource list --resource-group <name> --subscription <id>` |
| Details | `az resource show --ids <id>` |
| PIM eligible | `az rest --method GET --url https://management.azure.com/providers/Microsoft.Authorization/roleEligibilityScheduleInstances?api-version=2020-10-01&$filter=asTarget()` |

## GCP — Phase 2

**CLI:** `gcloud`.
**Login:** `gcloud auth login` + `gcloud auth application-default login`.

Planned wrappers: `gcloud organizations list`, `gcloud projects list`,
`gcloud asset search-all-resources`, `gcloud billing accounts list`.

## AWS — Phase 3

**CLI:** `aws` (v2).
**Login:** `aws configure sso` (recommended) or `aws configure`.

Planned wrappers: `aws organizations list-accounts`, `aws sts assume-role`,
per-service browsers (EC2, S3, Lambda, RDS), `aws ce get-cost-and-usage`.
