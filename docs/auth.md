# Authentication

cloudnav doesn't manage credentials of its own. Every cloud's SDK
auth chain is honoured as-is, so whatever your terminal session is
already authenticated against — CLI, Service Principal, federated
workload identity, IAM role from instance metadata — cloudnav uses
the same path.

`cloudnav doctor` shows the active method per cloud:

```
✓ azure  alice@example.com           · Azure CLI cached token
✓ gcp    sa@my-project.iam.gserviceaccount.com  · Service Account JSON (GOOGLE_APPLICATION_CREDENTIALS)
✓ aws    arn:aws:iam::123:user/alice · Default profile / SSO (~/.aws/credentials)
```

The probe runs through each cloud's SDK chain first; the CLI-based
check is only used as a fallback when the SDK can't resolve creds
(no env vars set, no cached file, no metadata server).

## Azure

Resolved by `azidentity.NewDefaultAzureCredential` in this order
(first match wins):

| # | Method | What sets it | Label `doctor` shows |
|---|---|---|---|
| 1 | **Workload Identity** (federated token file) | `AZURE_TENANT_ID` + `AZURE_CLIENT_ID` + `AZURE_FEDERATED_TOKEN_FILE` (set automatically in AKS, GitHub Actions OIDC) | `Workload Identity (federated token file)` |
| 2 | **Service Principal — client secret** | `AZURE_TENANT_ID` + `AZURE_CLIENT_ID` + `AZURE_CLIENT_SECRET` | `Service Principal (client secret env vars)` |
| 3 | **Service Principal — certificate** | `AZURE_TENANT_ID` + `AZURE_CLIENT_ID` + `AZURE_CLIENT_CERTIFICATE_PATH` | `Service Principal (client certificate)` |
| 4 | **Managed Identity** (IMDS) | `IDENTITY_ENDPOINT` set automatically on Azure VMs / App Service / Functions | `Managed Identity (IMDS)` |
| 5 | **Azure CLI** cached token | `az login` once | `Azure CLI cached token` |

Examples:

```bash
# Service Principal with secret
export AZURE_TENANT_ID=11111111-1111-1111-1111-111111111111
export AZURE_CLIENT_ID=22222222-2222-2222-2222-222222222222
export AZURE_CLIENT_SECRET=...
cloudnav doctor

# Federated workload identity (CI runners typically pre-set these)
export AZURE_FEDERATED_TOKEN_FILE=/var/run/secrets/azure/tokens/azure-identity-token
export AZURE_TENANT_ID=...
export AZURE_CLIENT_ID=...
cloudnav doctor
```

## GCP

Resolved by `google.FindDefaultCredentials` (the same chain
`gcloud` and every Google SDK use):

| # | Method | What sets it | Label `doctor` shows |
|---|---|---|---|
| 1 | **Service Account JSON** | `GOOGLE_APPLICATION_CREDENTIALS=/path/to/sa.json` (file's `type: service_account`) | `Service Account JSON (GOOGLE_APPLICATION_CREDENTIALS)` |
| 2 | **Workload Identity Federation** | `GOOGLE_APPLICATION_CREDENTIALS=...` pointing at an `external_account` JSON (GitHub Actions OIDC, AWS / Azure federation) | `Workload Identity Federation (external_account JSON)` |
| 3 | **Impersonated SA** | `impersonated_service_account` JSON file | `Impersonated Service Account JSON` |
| 4 | **Metadata server** (GCE / Cloud Run / GKE) | runs automatically on GCP infra | `Metadata Server (GCE / Cloud Run / GKE)` |
| 5 | **gcloud user creds** | `gcloud auth application-default login` once | `gcloud cached ADC (\`gcloud auth application-default login\`)` |

Examples:

```bash
# Service-account JSON
export GOOGLE_APPLICATION_CREDENTIALS=$HOME/.gcp/sa-cloudnav.json
cloudnav doctor

# Workload Identity Federation from GitHub Actions
# (token-source URL + audience handled by the runner)
export GOOGLE_APPLICATION_CREDENTIALS=$RUNNER_TEMP/wif.json
cloudnav doctor

# gcloud user creds (interactive)
gcloud auth application-default login
cloudnav doctor
```

## AWS

Resolved by `config.LoadDefaultConfig` (the same chain `aws-cli`
and every SDK use):

| # | Method | What sets it | Label `doctor` shows |
|---|---|---|---|
| 1 | **Web Identity / OIDC** (IRSA, GitHub Actions) | `AWS_WEB_IDENTITY_TOKEN_FILE` + `AWS_ROLE_ARN` | `Web Identity / OIDC (IRSA, GitHub Actions, etc.)` |
| 2 | **Temporary credentials** | `AWS_ACCESS_KEY_ID` + `AWS_SESSION_TOKEN` | `Temporary credentials (env vars)` |
| 3 | **Static IAM keys** | `AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY` | `Static IAM keys (AWS_ACCESS_KEY_ID env vars)` |
| 4 | **ECS task role** | runs automatically on Fargate / ECS tasks | `ECS task role (container metadata)` |
| 5 | **Profile** | `AWS_PROFILE=name` matching a section in `~/.aws/config` | `Shared profile: <name> (~/.aws/config)` |
| 6 | **Default profile / SSO** | `aws configure sso` once | `Default profile / SSO (~/.aws/credentials)` |
| 7 | **EC2 IMDS** | runs automatically on EC2 instances | resolved silently — IMDS surfaces as the active source when nothing above is set |

`AssumeRole` (cross-account elevation) is configured via a profile
(`role_arn`, `source_profile` keys in `~/.aws/config`); cloudnav
sees the resolved temporary credentials, the same way the AWS CLI
would.

Examples:

```bash
# Static keys
export AWS_ACCESS_KEY_ID=AKIA...
export AWS_SECRET_ACCESS_KEY=...
cloudnav doctor

# IRSA (k8s service-account-mounted token)
export AWS_WEB_IDENTITY_TOKEN_FILE=/var/run/secrets/eks.amazonaws.com/serviceaccount/token
export AWS_ROLE_ARN=arn:aws:iam::123456789012:role/CloudnavReader
cloudnav doctor

# Named profile (with optional AssumeRole config)
export AWS_PROFILE=cloudnav-readonly
cloudnav doctor
```

## Why both SDK and CLI checks?

cloudnav's resolution order is:

1. **SDK auth chain**, via the `Identifier` interface every provider
   implements. This is the path every cost / billing / advisor /
   metric / health call uses, so a green check here means the rest of
   cloudnav will work.
2. **CLI fallback**, only when the SDK chain returns no credentials.
   Useful on hosts where only `az login` / `gcloud auth login` /
   `aws configure sso` cached tokens exist and no env-var-driven
   path is configured.

Both produce the same `✓` row in `doctor`; the row's right-hand
label tells you which one resolved.

## Reading the label

`doctor` separates principal and method with `·`:

```
✓ azure  alice@example.com  · Azure CLI cached token
                            ^
                            method label — see tables above
```

If the principal looks unexpected (a Service Principal where you
expected your user account, or vice versa), check the env vars
listed for the matching label and unset whichever is overriding the
chain.
