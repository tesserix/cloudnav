# Adding resource-type aliases

The TYPE column on the resource list shows a short label (`vm`, `aks`,
`pep`, `s3`) instead of the raw provider type string
(`Microsoft.Compute/virtualMachines`, `ec2:instance`, …). The labels
come from a curated lookup map in
[`internal/tui/resource_types.go`](../internal/tui/resource_types.go).

This runbook is a five-minute guide for adding or fixing an alias.

---

## When to add one

You land on a resource whose TYPE column reads:

- the raw full type string (`Microsoft.Something/veryLongName`), or
- a suffix-only fallback that still isn't the label your team uses (e.g.
  `managedClusters` when everyone says "aks"),

…and you'd prefer the operator-community shorthand.

Everything not in the map already falls back to the segment after the
last `/`. That's a sensible default — **adding an alias is polish, not
a fix**. Keep the map curated; resist the urge to list every type a
cloud ships.

---

## Where the map lives

```
internal/tui/resource_types.go
└── typeAliases map[string]string   // lowercased type → short label
```

Rules:

- **Keys are lowercased.** The lookup is case-insensitive — if a cloud
  returns `Microsoft.Compute/VirtualMachines` or
  `compute.googleapis.com/Instance`, the map entry should be the
  all-lowercase form (`microsoft.compute/virtualmachines`,
  `compute.googleapis.com/instance`).
- **Values are short** — 3–8 cells. Longer aliases defeat the purpose.
  Prefer the shorthand operators already use in docs / terraform /
  SDK identifiers.
- **Values are unique-ish.** Two different services can share an alias
  only if context makes the column unambiguous (e.g. AWS and GCP both
  have `redis`; fine because the TYPE column is only shown inside one
  cloud at a time).

---

## Step-by-step

### 1. Find the canonical type string

Run cloudnav against a real resource of that kind. In the TUI, press
`i` (info) on the row — the detail pane prints the raw provider JSON.
Look for the `type` field (Azure / GCP) or the ARN (AWS).

Examples:

| Cloud | Source | Example |
|-------|--------|---------|
| Azure | `type` on ARM JSON | `Microsoft.Network/privateEndpoints` |
| GCP | `assetType` from Cloud Asset | `compute.googleapis.com/Instance` |
| AWS | ARN segments 3 + 6 | ARN `arn:aws:ec2:…:volume/vol-xxx` → `ec2:volume` |

### 2. Pick a short label

Follow existing conventions in the map. If the community uses
`terraform` or `kubectl`-style shorthand, use that. A few rules of
thumb:

- **Service prefixes trump resource type.** `Microsoft.KeyVault/vaults`
  → `kv`, not `vault`.
- **Avoid collisions within one cloud.** Don't add `sa` for both
  `storageAccounts` and `serviceAccounts` — pick different labels.
- **Plain English beats acronyms when the service name already is
  short.** `redis` → `redis` (not `rds`, which is a different AWS
  service).

### 3. Add the entry

Open `internal/tui/resource_types.go` and add the pair to the right
bucket (Azure / GCP / AWS). Example:

```go
// Azure — Extended network
"microsoft.network/natgateways":  "natgw",
"microsoft.network/dnsresolvers": "dnsres", // ← new line
```

### 4. Lock it with a test

Open `internal/tui/resource_types_test.go` and extend the matching
`TestFriendlyType{Azure|GCP|AWS}` case table:

```go
{"Microsoft.Network/dnsResolvers", "dnsres"},
```

The test feeds the original cased input so regressions in the
case-insensitive lookup are caught.

### 5. Verify

```bash
go test -race ./internal/tui/ -run FriendlyType -v
go build ./...
```

That's it. A single PR per alias (or per batch) is fine.

---

## Removing or renaming

Aliases are stable user-facing strings — people scan the TYPE column
by muscle memory. Don't rename casually. If you have a good reason:

1. Open a PR with the rename, link an issue explaining why.
2. Update any docs that quoted the old label (grep the repo).
3. Release-note the change in `CHANGELOG.md` under `Changed`.

---

## Fallback behaviour (what unknown types do)

`friendlyType()` in the same file:

```go
// Unknown type → segment after the last slash.
// "Microsoft.NewService/widgets"       → "widgets"
// "compute.googleapis.com/NewThing"    → "NewThing"
// "plainstring-no-separator"           → "plainstring-no-separator"
```

So nothing is ever hidden — worst case a user sees a medium-short
fallback label until the alias is added.

---

## Common buckets (at a glance)

The map is commented per category so new entries have an obvious
home. Current buckets:

**Azure**: Compute · Container · Network · Storage/Data · Web/Apps ·
Messaging · Ops/Observability · Identity/Security · AI/ML/Analytics ·
Extended compute (Arc/AVD) · Extended network · Data protection.

**GCP**: Compute Engine · Network · Containers/Serverless · Storage/Data ·
IAM/Security · Messaging/Observability · CI-CD (Artifact Registry /
Cloud Build) · Service Directory / Private · AI/ML/Big data ·
Billing/Admin.

**AWS**: Compute · Network · Containers/Serverless · Storage/Data ·
IAM/Security · Messaging/Observability/Ops · DevOps (Code*) ·
ML/Analytics · Integration/Streaming · Transfer/Media ·
IoT/Connect/WorkSpaces/Org.

When you add several entries, keep them grouped under the right
`// Azure — …` banner comment so diffs stay readable.

---

## Why curated, not generated

Short answer: there is no deterministic rule that produces `pep` from
`Microsoft.Network/privateEndpoints`. The aliases follow community
convention (what folks type in Terraform config, what
support-engineers say in tickets, what the SDKs name their modules
after), not an algorithm over the type string. Attempting to derive
them would give worse output and require re-deriving the same map
you'd otherwise curate once.

The curated approach also lets us cluster shared aliases across
clouds (`redis` in Azure + GCP + AWS, `vm` in Azure + GCP) so the
TYPE column reads consistently when users switch surfaces.
