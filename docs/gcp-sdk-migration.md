# GCP SDK migration plan

The Azure provider went through a full SDK migration in the 0.21.x
series — `az` CLI shelling replaced by `azcore` / `azidentity` /
`armresources` etc. The GCP provider hasn't gone through the same
yet: every call still routes through `g.gcloud.Run(ctx, …)`, which
spawns a subprocess and parses its stdout. This doc is the roadmap
for bringing GCP up to the same shape, plus the closest-equivalent
designs for delete and lock semantics that GCP doesn't natively
mirror from Azure.

## Why migrate

- **Latency.** Each `gcloud.Run` shells out to a Python CLI; cold
  starts are ~250–600 ms before any API call lands. The Go SDK
  reuses an HTTP/2 client across the process.
- **Auth.** SDKs use Application Default Credentials directly — same
  source `gcloud` reads from — so cross-tenant / service-account /
  workload-identity all keep working without re-shelling per call.
- **Errors.** The SDK returns typed errors with status codes; the
  CLI returns stderr we have to regex.
- **Concurrency.** SDK clients are safe to share across goroutines.
  `gcloud.Run` requires its own subprocess per call.

## Target SDKs

| Surface | SDK module | Replaces |
|---|---|---|
| Projects + folders | `cloud.google.com/go/resourcemanager/apiv3` | `gcloud projects list` / `folders list` |
| Asset Inventory | `cloud.google.com/go/asset/apiv1` | `gcloud asset search-all-resources` |
| Compute (VMs) | `cloud.google.com/go/compute/apiv1` | `gcloud compute instances ...` |
| Recommender (Advisor) | `cloud.google.com/go/recommender/apiv1` | `gcloud recommender ...` |
| Billing | `cloud.google.com/go/billing/apiv1` | `gcloud billing ...` |
| BigQuery (cost queries) | `cloud.google.com/go/bigquery` | `gcloud alpha bq query ...` |
| IAM (roles, conditions) | `cloud.google.com/go/iam/apiv2` | `gcloud iam ...` |
| Monitoring (Metricser) | `cloud.google.com/go/monitoring/apiv3/v2` | `gcloud monitoring metrics-scopes ...` |
| Cloud Liens (lock equiv.) | `cloud.google.com/go/resourcemanager/apiv3` (Liens RPCs) | new — no current code |

Each SDK pulls a couple of MB of generated client code. We add them
**one PR at a time** so a regression is bisectable.

## Migration order

The order is chosen so each step is **independently shippable**, the
foundation lands first, and the heavier deps (BigQuery,
Monitoring) come last when the harness around them is already
proven.

1. **Foundation** — `resourcemanager/apiv3` + `Root()` and the
   project-listing path. Verify perf delta vs gcloud (target: 5–10×
   on cold start). Keep a `cli.Runner` fallback for environments
   where ADC isn't set up so cloudnav doesn't break for users who
   only have `gcloud` cached creds.
2. **Asset Inventory** — `asset/apiv1` for the resource enumeration
   under projects. Biggest TUI impact (every drill into a project
   uses this).
3. **Compute SDK** — VM ops (`StartVM` / `StopVM` / `ShowVM`).
   Removes a subprocess from the `x` exec path.
4. **Recommender SDK** — Advisor. Useful when the user opens the
   `A` overlay; today a 1–2 s lag.
5. **Billing + BigQuery** — `cost projects` (CLI) and the `B`
   overlay.
6. **Delete dispatcher** — see "Delete model" below.
7. **Project liens (lock equivalent)** — see "Lock model" below.
8. **Monitoring SDK** — Metricser / `m` overlay.

## Delete model — formalise as `Deleter`

Today `internal/tui/delete.go` does a type-assertion to
`*azure.Azure`. That's the right shape to extract into an interface:

```go
// internal/provider/provider.go
type Deleter interface {
    DeleteResource(ctx context.Context, n Node) error
    DeleteResourceGroup(ctx context.Context, n Node) error // optional, returns ErrNotSupported on GCP
}
```

GCP has no single delete RPC; each resource type has its own. The
provider would dispatch on `Node.Meta["type"]`:

| type prefix | SDK call |
|---|---|
| `compute.googleapis.com/Instance` | `compute.InstancesClient.Delete` |
| `storage.googleapis.com/Bucket` | `storage.Client.Bucket(name).Delete` |
| `container.googleapis.com/Cluster` | `container.ClusterManagerClient.DeleteCluster` |
| `bigquery.googleapis.com/Dataset` | `bigquery.Client.Dataset(id).Delete` |
| (else) | `ErrNotSupported` — UI shows portal hand-off |

Project deletion (the closest analog to RG deletion) goes through
`resourcemanager.ProjectsClient.DeleteProject` and respects liens,
which segues into:

## Lock model — project liens, IAM deny, org policy

GCP has no management-lock primitive equivalent to
`Microsoft.Authorization/locks`. The closest analogs:

- **Project lien** (`resourcemanager.LiensClient.CreateLien`) — a
  single string that prevents `cloudresourcemanager.projects.delete`
  on the bound project. Closest 1:1 mapping to "CanNotDelete" lock,
  scoped at project granularity. **This is what `cloudnav` should
  surface as a "lock" on GCP.**
- **IAM deny policies** — broader than locks; can deny any
  permission on a resource. Powerful but not directly comparable to
  "delete-only" locks. Out of scope for the lock UI; consider an
  `iam-deny` advisor card later.
- **Org policy constraints** — org-wide guardrails. Different
  audience (admins, not navigators).

Proposed UI: when the user presses `L` on a GCP project, show an
overlay listing `Liens.List` results (origin / reason / restricts).
Pressing `L` on a resource (compute instance, bucket) shows
"liens are project-scoped on GCP — see <project>" and links into
the project view.

## Filter & sort parity

The Azure list views use the `s` cycle (name → cost → state) and
the `/` filter. GCP frames inherit `/` automatically (it's frame-
agnostic) but the sort cycle's `state` column has no GCP analog.
Two options:

1. Skip the `state` column for GCP projects (cycle becomes name →
   cost → name).
2. Map state to `lifecycleState` for projects (`ACTIVE`,
   `DELETE_REQUESTED`, …) and to a per-type field for resources
   (instance status, bucket lifecycle).

Option 2 is more useful but requires per-type mapping in
`rowsFromNodes`. Defer to step 2 of the migration order — Asset
Inventory already returns the relevant fields, we just need to
read them.

## Out of scope for this doc

- VPC Service Controls (different security model than locks).
- Org-policy enforcement views.
- Cloud Run / GKE workload-level navigation (currently filtered
  out in `gcp.go` lines 173–245 by design).

## Tracking

Each step gets a small dedicated CHANGELOG block; the per-PR
commits land sequentially on `main` (no long-lived branch). A
session can pick up at the next unchecked step without context
from previous PRs.
