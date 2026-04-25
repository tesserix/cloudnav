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

1. ✅ **Foundation** — `resourcemanager/apiv3` + `Root()` and the
   project-listing path. Shipped in v0.22.34. Falls back to
   `gcloud projects list` when ADC isn't available.
2. ✅ **Asset Inventory** — `asset/apiv1` for the resource
   enumeration under projects. Shipped in v0.22.34. ~3× faster
   than `gcloud asset search-all-resources` on a 5k-asset project.
3. ✅ **Compute SDK** — VM ops (`StartVM` / `StopVM` / `ShowVM` /
   `ListVMs`). Shipped in v0.22.35. AggregatedList collapses
   per-zone fan-outs into one RPC.
4. ✅ **Recommender SDK** — Advisor (`Recommendations`). Shipped
   in v0.22.35. Per-recommender catalog parallelism now runs
   against the SDK; gcloud fallback stays for unenabled APIs.
5. ✅ **Billing + BigQuery** — `cost projects` CLI and the `B`
   overlay. Shipped in v0.22.36. Typed row scanning, no
   subprocess, `<host_project>.<dataset>.<table>` shape parses
   the host project automatically.
6. ✅ **Delete dispatcher** — see "Delete model" below. Shipped
   in v0.22.36 — formal `provider.Deleter` interface, Azure
   adapter wrapping the existing methods, GCP per-asset-type
   dispatch (compute instances, storage buckets, projects).
7. ✅ **Project liens (lock equivalent)** — see "Lock model"
   below. Shipped in v0.22.36 — `provider.Locker` interface
   plus GCP implementation via `gcloud alpha resource-manager
   liens` (the v3 SDK doesn't expose Liens; v1 REST is the only
   alternative and would mean a second auth pool).
8. ✅ **Monitoring SDK** — Metricser / `m` overlay. Shipped in
   v0.22.36 — Cloud Monitoring v3 `ListTimeSeries` with
   AlignmentPeriod=300s, ALIGN_RATE / ALIGN_MEAN matching the
   gcloud CLI behaviour bit-for-bit.

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

## Phases 9–12 + new capabilities (v0.22.37)

- ✅ **Phase 9 — Folders SDK.** `resourcemanager.FoldersClient`
  for org-level folder traversal; `SearchProjects` with
  `parent:folders/<id>` for sub-projects.
- ✅ **Phase 10 — Privileged Access Manager SDK.** PAM
  entitlements + grants via
  `cloud.google.com/go/privilegedaccessmanager/apiv1`. Conditional
  IAM fallback stays on the gcloud path.
- ✅ **Phase 11 — ADC-based `LoggedIn`.** Drops the `gcloud auth
  list` subprocess from cloudnav startup. Uses
  `golang.org/x/oauth2/google.FindDefaultCredentials` and mints
  one access token to verify creds end-to-end.
- ✅ **Phase 12 — Cloud Billing SDK.** `cloudbilling.v1`
  `GetProjectBillingInfo` for the auto-detect path that resolves
  the BQ export table from the active gcloud project.

### New capabilities (not previously implemented)

- ✅ **Service Health for GCP** — implements
  `provider.HealthEventer` via
  `cloud.google.com/go/servicehealth/apiv1.ListEvents` across all
  accessible projects. The `H` overlay now lights up on GCP at
  parity with Azure / AWS.
- ✅ **Cloud Billing Budgets** — `B` overlay now reads real budget
  caps via `cloud.google.com/go/billing/budgets/apiv1` instead of
  parsing CLI JSON. Currency derived from the SDK Money type.

## Cross-cloud navigation hierarchy reference

| Cloud | Top → Bottom |
|---|---|
| Azure | Tenant → Subscription → ResourceGroup → Resource |
| GCP   | Organization → Folder → Project → Resource |
| AWS   | Organization → OU → Account → Region → Resource |

cloudnav normalises these into one `Kind` enum + the same drill
path so users press the same keys for the same operations across
clouds.

## Tracking

Each step gets a small dedicated CHANGELOG block; the per-PR
commits land sequentially on `main` (no long-lived branch). A
session can pick up at the next unchecked step without context
from previous PRs.
