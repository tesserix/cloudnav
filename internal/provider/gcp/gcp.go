// Package gcp implements provider.Provider by wrapping the `gcloud` CLI.
package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/tesserix/cloudnav/internal/cli"
	"github.com/tesserix/cloudnav/internal/provider"
)

type GCP struct {
	gcloud       *cli.Runner
	billingTable string // populated via SetBillingTable from cfg; env var still overrides
	sdk          sdkClients
}

func New() *GCP {
	r := cli.New("gcloud")
	r.Timeout = 3 * time.Minute
	return &GCP{gcloud: r}
}

func (g *GCP) Name() string { return "gcp" }

func (g *GCP) LoggedIn(ctx context.Context) error {
	// SDK fast path — Application Default Credentials. Resolves
	// the active account from the same source `gcloud auth list`
	// reads (gcloud-cached creds, service-account JSON, workload
	// identity, etc.) but without forking a Python interpreter on
	// every cloudnav startup.
	if err := g.checkADC(ctx); err == nil {
		return nil
	}
	out, err := g.gcloud.Run(ctx, "auth", "list",
		"--filter=status:ACTIVE",
		"--format=value(account)",
	)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return fmt.Errorf("gcloud: no active account (run `gcloud auth login`)")
	}
	return nil
}

// LoginCommand returns the argv that runs gcloud's interactive login.
func (g *GCP) LoginCommand() (string, []string) {
	return "gcloud", []string{"auth", "login"}
}

// InstallHint points first-time users at the Google Cloud SDK installer.
func (g *GCP) InstallHint() string {
	return "install Google Cloud SDK: https://cloud.google.com/sdk/docs/install"
}

// InstallPlan picks a per-OS install method for gcloud.
func (g *GCP) InstallPlan(goos string) ([]provider.InstallStep, bool) {
	switch goos {
	case "darwin":
		return []provider.InstallStep{{
			Description: "brew install --cask google-cloud-sdk",
			Bin:         "brew", Args: []string{"install", "--cask", "google-cloud-sdk"},
		}}, true
	case "linux":
		if _, err := exec.LookPath("brew"); err == nil {
			return []provider.InstallStep{{
				Description: "brew install --cask google-cloud-sdk",
				Bin:         "brew", Args: []string{"install", "--cask", "google-cloud-sdk"},
			}}, true
		}
		return []provider.InstallStep{{
			Description: "run the official Google Cloud SDK install script (interactive)",
			Bin:         "sh", Args: []string{"-c", "curl -sSL https://sdk.cloud.google.com | bash"},
		}}, true
	case "windows":
		return []provider.InstallStep{{
			Description: "winget install Google.CloudSDK",
			Bin:         "winget", Args: []string{"install", "-e", "--id", "Google.CloudSDK"},
		}}, true
	}
	return nil, false
}

type projectJSON struct {
	ProjectID      string `json:"projectId"`
	Name           string `json:"name"`
	ProjectNumber  string `json:"projectNumber"`
	LifecycleState string `json:"lifecycleState"`
	CreateTime     string `json:"createTime"`
}

func (g *GCP) Root(ctx context.Context) ([]provider.Node, error) {
	// Folder mode: when CLOUDNAV_GCP_ORG is set the top level becomes the
	// folders directly under that org, and the user drills into a folder
	// to see its projects. Any permission or lookup failure falls through
	// to the flat project list so the TUI never breaks on standalone
	// accounts that happen to have stale env config.
	if org := orgID(); org != "" {
		if folders, _ := g.listFolders(ctx, org); len(folders) > 0 {
			return folders, nil
		}
	}
	// SDK fast path: Resource Manager v3 SearchProjects RPC. Authenticated
	// via Application Default Credentials (the same source `gcloud` reads
	// from), reuses one HTTP/2 connection across the process, returns
	// typed errors. Falls back to `gcloud projects list` when ADC isn't
	// configured (CI, fresh machines, service-account-less hosts).
	if nodes, sdkUsable, err := g.listProjectsSDK(ctx); sdkUsable && err == nil {
		return nodes, nil
	}
	out, err := g.gcloud.Run(ctx, "projects", "list", "--format=json")
	if err != nil {
		return nil, err
	}
	return parseProjects(out)
}

func parseProjects(data []byte) ([]provider.Node, error) {
	var ps []projectJSON
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, fmt.Errorf("parse gcloud projects: %w", err)
	}
	nodes := make([]provider.Node, 0, len(ps))
	for _, p := range ps {
		meta := map[string]string{
			"projectNumber": p.ProjectNumber,
			"createTime":    p.CreateTime,
		}
		if p.CreateTime != "" {
			meta["createdTime"] = p.CreateTime
		}
		nodes = append(nodes, provider.Node{
			ID:    p.ProjectID,
			Name:  p.Name,
			Kind:  provider.KindProject,
			State: p.LifecycleState,
			Meta:  meta,
		})
	}
	return nodes, nil
}

func (g *GCP) Children(ctx context.Context, parent provider.Node) ([]provider.Node, error) {
	switch parent.Kind {
	case provider.KindProject:
		return g.resources(ctx, parent)
	case provider.KindFolder:
		return g.folderChildren(ctx, parent)
	default:
		return nil, fmt.Errorf("gcp: no children for kind %q", parent.Kind)
	}
}

type assetJSON struct {
	Name        string            `json:"name"`
	AssetType   string            `json:"assetType"`
	Location    string            `json:"location"`
	DisplayName string            `json:"displayName"`
	Project     string            `json:"project"`
	CreateTime  string            `json:"createTime"`
	UpdateTime  string            `json:"updateTime"`
	Labels      map[string]string `json:"labels"`
}

// assetPageLimit caps how many resources we fetch per project drill. gcloud
// auto-paginates with a 100-default page size, so a project with ~5k assets
// takes ~4 minutes to enumerate — unacceptable for an interactive TUI. 500
// feels right: large projects load in a few seconds, and search / filter
// inside the TUI handles anything the user would otherwise need the full
// list for.
const assetPageLimit = 500

// assetTypeWhitelist is the default set of first-party GCP asset types the
// resource view lists. Kubernetes workload resources (Pod, Deployment,
// ReplicaSet, Endpoints, Service) live under k8s.io/* and apps.k8s.io/*
// asset types — listing them here would drown out the actual GCP resources
// (GKE clusters, Cloud SQL, VMs, buckets, etc.) on any project with a busy
// GKE cluster. Users who need them run kubectl instead.
//
// The env var CLOUDNAV_GCP_ASSET_TYPES="*" opts back into the unfiltered
// view when someone actually does want the k8s rows.
var assetTypeWhitelist = []string{
	// Compute
	"compute.googleapis.com/Instance",
	"compute.googleapis.com/InstanceGroup",
	"compute.googleapis.com/InstanceGroupManager",
	"compute.googleapis.com/InstanceTemplate",
	"compute.googleapis.com/Disk",
	"compute.googleapis.com/Network",
	"compute.googleapis.com/Subnetwork",
	"compute.googleapis.com/Firewall",
	"compute.googleapis.com/Address",
	"compute.googleapis.com/ForwardingRule",
	"compute.googleapis.com/TargetPool",
	"compute.googleapis.com/BackendService",
	"compute.googleapis.com/UrlMap",
	"compute.googleapis.com/SslCertificate",
	"compute.googleapis.com/Router",
	"compute.googleapis.com/VpnTunnel",
	// Containers
	"container.googleapis.com/Cluster",
	"container.googleapis.com/NodePool",
	// Serverless
	"run.googleapis.com/Service",
	"run.googleapis.com/Revision",
	"run.googleapis.com/Job",
	"cloudfunctions.googleapis.com/Function",
	"workflows.googleapis.com/Workflow",
	// Data stores — Asset Inventory's search surface only includes a subset
	// of these resource types; see cloud.google.com/asset-inventory/docs/
	// supported-asset-types. sqladmin.googleapis.com/Database,
	// aiplatform.googleapis.com/Notebook, sourcerepo/Repository and
	// cloudcdn/CdnPolicy aren't searchable today and fail the whole call
	// with INVALID_ARGUMENT — keep them out of the whitelist.
	"sqladmin.googleapis.com/Instance",
	"spanner.googleapis.com/Instance",
	"spanner.googleapis.com/Database",
	"bigtableadmin.googleapis.com/Instance",
	"bigtableadmin.googleapis.com/Cluster",
	"bigtableadmin.googleapis.com/Table",
	"redis.googleapis.com/Instance",
	"memcache.googleapis.com/Instance",
	"firestore.googleapis.com/Database",
	"storage.googleapis.com/Bucket",
	// Analytics & ML
	"bigquery.googleapis.com/Dataset",
	"bigquery.googleapis.com/Table",
	"dataflow.googleapis.com/Job",
	"dataproc.googleapis.com/Cluster",
	"composer.googleapis.com/Environment",
	"aiplatform.googleapis.com/Endpoint",
	"aiplatform.googleapis.com/Model",
	// Messaging
	"pubsub.googleapis.com/Topic",
	"pubsub.googleapis.com/Subscription",
	// DevOps / registries
	"artifactregistry.googleapis.com/Repository",
	"cloudbuild.googleapis.com/BuildTrigger",
	// Networking
	"dns.googleapis.com/ManagedZone",
	// Security / IAM
	"iam.googleapis.com/ServiceAccount",
	"iam.googleapis.com/Role",
	"secretmanager.googleapis.com/Secret",
	"cloudkms.googleapis.com/KeyRing",
	"cloudkms.googleapis.com/CryptoKey",
	// Observability
	"monitoring.googleapis.com/AlertPolicy",
	"monitoring.googleapis.com/Dashboard",
	"monitoring.googleapis.com/NotificationChannel",
	// Billing / project
	"cloudresourcemanager.googleapis.com/Project",
	"cloudresourcemanager.googleapis.com/Folder",
}

// resolveAssetTypes returns the --asset-types argument to pass to gcloud.
// CLOUDNAV_GCP_ASSET_TYPES wins when set: "*" (or "all") means unfiltered,
// any comma-separated list overrides the default whitelist.
func resolveAssetTypes() string {
	if v := strings.TrimSpace(os.Getenv("CLOUDNAV_GCP_ASSET_TYPES")); v != "" {
		if v == "*" || strings.EqualFold(v, "all") {
			return ""
		}
		return v
	}
	return strings.Join(assetTypeWhitelist, ",")
}

func (g *GCP) resources(ctx context.Context, project provider.Node) ([]provider.Node, error) {
	// Cloud Asset API (cloudasset.googleapis.com) has to be enabled on the
	// project. The `--limit` bounds very large projects that would otherwise
	// spend minutes paginating millions of assets; a 90s ctx keeps the TUI
	// responsive even when the API is slow. On failure we surface a concise,
	// actionable error — enabling the API or the permission the user needs —
	// instead of dumping the raw gcloud stack.
	callCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	// SDK fast path. Cloud Asset Inventory v1 SearchAllResources via
	// cloud.google.com/go/asset — single HTTP/2 connection reused
	// across the process, ~3× faster than spawning gcloud per drill on
	// a 5k-asset project. Falls through to the gcloud CLI path on any
	// SDK failure (no ADC, API not enabled, transient network error)
	// so the user keeps a working drill in every environment.
	if nodes, sdkUsable, err := g.searchAssetsSDK(callCtx, project, resolveAssetTypes(), assetPageLimit); sdkUsable && err == nil {
		if len(nodes) > assetPageLimit {
			nodes = nodes[:assetPageLimit]
			if len(nodes) > 0 {
				if nodes[0].Meta == nil {
					nodes[0].Meta = map[string]string{}
				}
				nodes[0].Meta["partial"] = fmt.Sprintf("%d", assetPageLimit)
			}
		}
		return nodes, nil
	}
	// Request limit+1 so we can tell the caller when the project has more
	// resources than we're showing.
	// --page-size raises the server page from the 100 default so the 500
	// cap lands in one round trip instead of six. On a 5k-asset project
	// this drops drill time from ~32s to ~3s.
	baseArgs := []string{
		"asset", "search-all-resources",
		"--scope=projects/" + project.ID,
		fmt.Sprintf("--limit=%d", assetPageLimit+1),
		fmt.Sprintf("--page-size=%d", assetPageLimit),
		"--format=json",
	}
	args := baseArgs
	if at := resolveAssetTypes(); at != "" {
		args = append(args, "--asset-types="+at)
	}
	out, err := g.gcloud.Run(callCtx, args...)
	if err != nil {
		// Asset Inventory periodically renames or drops searchable types;
		// when the filter is the reason the call failed, retry unfiltered
		// so the user still gets a usable list (just noisier).
		if isInvalidAssetType(err) && len(args) > len(baseArgs) {
			out, err = g.gcloud.Run(callCtx, baseArgs...)
		}
		if err != nil {
			return nil, translateAssetError(err, project.ID)
		}
	}
	nodes, err := parseAssets(out, project)
	if err != nil {
		return nil, err
	}
	if len(nodes) > assetPageLimit {
		// Drop the sentinel and mark the frame as partial via the last node's
		// meta — the TUI picks this up to render a "showing N of many" hint.
		nodes = nodes[:assetPageLimit]
		nodes[0].Meta["partial"] = fmt.Sprintf("%d", assetPageLimit)
	}
	return nodes, nil
}

// isInvalidAssetType detects the "No supported asset type matches" shape the
// Asset Inventory API returns when one of the types in --asset-types isn't
// searchable. The whole call fails rather than ignoring the unknown entry,
// so we catch it and retry without the filter.
func isInvalidAssetType(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "INVALID_ARGUMENT") && strings.Contains(s, "asset type")
}

// translateAssetError rewrites the most common Cloud Asset failure modes into
// a single actionable sentence. Anything we don't recognise falls through
// with the original gcloud message so the user can still debug.
func translateAssetError(err error, projectID string) error {
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "cloudasset.googleapis.com") && (strings.Contains(s, "not been used") || strings.Contains(s, "disabled")):
		return fmt.Errorf("gcp: Cloud Asset API is disabled on %s — run: gcloud services enable cloudasset.googleapis.com --project=%s", projectID, projectID)
	case strings.Contains(s, "permission") && strings.Contains(s, "cloudasset"):
		return fmt.Errorf("gcp: your account lacks cloudasset.assets.searchAll on %s — ask an admin for roles/cloudasset.viewer or roles/viewer", projectID)
	case strings.Contains(s, "context deadline exceeded"):
		return fmt.Errorf("gcp: asset search for %s timed out after 90s — project may have too many assets; try `gcloud asset search-all-resources --scope=projects/%s` directly to inspect", projectID, projectID)
	}
	return fmt.Errorf("gcp asset search on %s: %w", projectID, err)
}

func parseAssets(data []byte, project provider.Node) ([]provider.Node, error) {
	var assets []assetJSON
	if err := json.Unmarshal(data, &assets); err != nil {
		return nil, fmt.Errorf("parse gcloud asset search: %w", err)
	}
	nodes := make([]provider.Node, 0, len(assets))
	parent := project
	for _, a := range assets {
		name := a.DisplayName
		if name == "" {
			name = shortName(a.Name)
		}
		meta := map[string]string{
			"type":    a.AssetType,
			"project": project.ID,
		}
		if a.CreateTime != "" {
			meta["createdTime"] = a.CreateTime
		}
		if a.UpdateTime != "" {
			meta["changedTime"] = a.UpdateTime
		}
		if tagsStr := formatGCPLabels(a.Labels); tagsStr != "" {
			meta["tags"] = tagsStr
		}
		nodes = append(nodes, provider.Node{
			ID:       a.Name,
			Name:     name,
			Kind:     provider.KindResource,
			Location: a.Location,
			State:    shortType(a.AssetType),
			Parent:   &parent,
			Meta:     meta,
		})
	}
	return nodes, nil
}

func (g *GCP) PortalURL(n provider.Node) string {
	switch n.Kind {
	case provider.KindProject:
		return "https://console.cloud.google.com/home/dashboard?project=" + n.ID
	case provider.KindResource:
		if p := n.Meta["project"]; p != "" {
			return "https://console.cloud.google.com/welcome?project=" + p
		}
		return "https://console.cloud.google.com/"
	default:
		return "https://console.cloud.google.com/"
	}
}

func (g *GCP) Details(ctx context.Context, n provider.Node) ([]byte, error) {
	switch n.Kind {
	case provider.KindProject:
		return g.gcloud.Run(ctx, "projects", "describe", n.ID, "--format=json")
	case provider.KindResource:
		return g.gcloud.Run(ctx, "asset", "search-all-resources",
			"--scope=projects/"+n.Meta["project"],
			"--query=name:"+n.ID,
			"--format=json",
		)
	default:
		return nil, fmt.Errorf("gcp: no detail view for kind %q", n.Kind)
	}
}

// formatGCPLabels renders a GCP labels map as a stable, compact
// "k=v, k=v" string for the TAGS column. Keys sort alphabetically so the
// rendering is deterministic and matches Azure's convention.
func formatGCPLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(k)
		if v := labels[k]; v != "" {
			b.WriteByte('=')
			b.WriteString(v)
		}
	}
	return b.String()
}

// shortType turns "compute.googleapis.com/Instance" into "Instance".
func shortType(t string) string {
	if i := strings.LastIndex(t, "/"); i >= 0 {
		return t[i+1:]
	}
	return t
}

// shortName turns "//compute.googleapis.com/projects/p/zones/z/instances/name"
// into "name".
func shortName(full string) string {
	if i := strings.LastIndex(full, "/"); i >= 0 {
		return full[i+1:]
	}
	return full
}
