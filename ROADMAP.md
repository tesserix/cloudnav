# Roadmap

## Phase 0 — Scaffold · done

- Repo, CI, release pipeline, Homebrew tap.
- Cobra root with `doctor`, `version`, `completion`.
- Bubbletea shell rendering a cloud picker.

## Phase 1 — Azure · current

- `az account list` → tenants / subscriptions.
- `az group list` → resource groups per subscription.
- `az resource list` → resources per group.
- Inline 30-day cost column via `az costmanagement query`.
- `o` opens the selected entity in `https://portal.azure.com`.
- `x` runs any `az` command inside the current scope.
- `p` lists and activates PIM-eligible roles.

## Phase 2 — GCP

- `gcloud organizations list` → orgs.
- `gcloud projects list` → projects (scoped to org if chosen).
- `gcloud asset search-all-resources` → resources per project.
- Billing via `gcloud billing accounts list` + `gcloud alpha billing`.
- `p` → temporary IAM binding with condition (JIT elevation).

## Phase 3 — AWS

- `aws organizations list-accounts` → accounts.
- `aws sts assume-role` / SSO for cross-account reads.
- Region picker, EC2 / S3 / Lambda / RDS browsers.
- Cost Explorer via `aws ce get-cost-and-usage`.

## Phase 4 — IAM provisioning

- `cloudnav iam bootstrap <cloud> --scope ... --preset ...`
- Presets: `viewer`, `cost-reader`, `security-auditor`, `cloudnav` (minimum needed for the tool).
- Azure Service Principal with federated credentials (OIDC).
- GCP Service Account with Workload Identity Federation.
- AWS IAM Role trust policy for GitHub OIDC.

## Phase 5 — Cross-cloud

- Unified cost dashboard (all clouds side by side).
- Global fuzzy search across tenants / projects / accounts.
- Bookmarks synced via optional config file.

## Non-goals

- Mutating cloud state beyond IAM provisioning.
- Re-implementing what the official CLIs already do.
- Web UI.
