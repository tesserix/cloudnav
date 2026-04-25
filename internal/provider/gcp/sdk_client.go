package gcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	asset "cloud.google.com/go/asset/apiv1"
	assetpb "cloud.google.com/go/asset/apiv1/assetpb"
	resourcemanager "cloud.google.com/go/resourcemanager/apiv3"
	resourcemanagerpb "cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	"google.golang.org/api/iterator"

	"github.com/tesserix/cloudnav/internal/provider"
)

// sdkClients lazily wraps the Google Cloud Go SDK client constructors
// cloudnav uses. Each client is built once per process under the once
// guard so we don't pay the auth-handshake cost per call. Application
// Default Credentials (the same source `gcloud` reads from) are used —
// no separate config required.
//
// When ADC isn't available (CI without a service account, freshly
// installed gcloud, etc.) the constructor returns an error; callers
// fall back to the cli.Runner subprocess path so cloudnav stays
// usable.
type sdkClients struct {
	once     sync.Once
	err      error
	projects *resourcemanager.ProjectsClient

	assetOnce sync.Once
	assetErr  error
	asset     *asset.Client
}

// projectsClient returns the lazy-initialised Projects client.
// Returns the same error on every call after the first failure so
// repeat attempts don't keep paying the auth probe latency.
func (g *GCP) projectsClient(ctx context.Context) (*resourcemanager.ProjectsClient, error) {
	g.sdk.once.Do(func() {
		// Per-call timeout is sized for the slowest Cloud API hop in
		// the cluster (us-central1 from EU is ~250 ms RTT); 10 s is
		// loose enough to absorb token-refresh on cold start without
		// hanging the TUI.
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		client, err := resourcemanager.NewProjectsClient(c)
		if err != nil {
			g.sdk.err = err
			return
		}
		g.sdk.projects = client
	})
	return g.sdk.projects, g.sdk.err
}

// assetClient returns the lazy-initialised Asset Inventory client.
// Same lazy + cache-error pattern as projectsClient.
func (g *GCP) assetClient(ctx context.Context) (*asset.Client, error) {
	g.sdk.assetOnce.Do(func() {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		client, err := asset.NewClient(c)
		if err != nil {
			g.sdk.assetErr = err
			return
		}
		g.sdk.asset = client
	})
	return g.sdk.asset, g.sdk.assetErr
}

// Close releases the SDK client connections. Safe to call multiple
// times. Currently a no-op when SDK clients were never opened (env
// without ADC).
func (g *GCP) Close() error {
	var firstErr error
	if g.sdk.projects != nil {
		if err := g.sdk.projects.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if g.sdk.asset != nil {
		if err := g.sdk.asset.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := closeComputeClient(); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := closeRecommenderClient(); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := closeBQClient(); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := closeMonitoringClient(); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := closeStorageClient(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// listProjectsSDK returns every project the caller has read access
// to via the Resource Manager v3 SearchProjects RPC. Returns
// (nil, false, err) when the SDK isn't usable so the caller can
// shell out to `gcloud projects list` instead.
func (g *GCP) listProjectsSDK(ctx context.Context) ([]provider.Node, bool, error) {
	client, err := g.projectsClient(ctx)
	if err != nil || client == nil {
		return nil, false, err
	}
	it := client.SearchProjects(ctx, &resourcemanagerpb.SearchProjectsRequest{})
	out := make([]provider.Node, 0, 32)
	for {
		p, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, true, err
		}
		// SDK projects use ACTIVE / DELETE_REQUESTED / DELETE_IN_PROGRESS
		// for State; Azure uses Enabled/Disabled. We surface the SDK
		// value verbatim — the TUI maps both via stateBadge.
		state := p.State.String()
		meta := map[string]string{
			"projectNumber": parseProjectNumber(p.Name),
			"createTime":    p.CreateTime.AsTime().Format(time.RFC3339),
			"createdTime":   p.CreateTime.AsTime().Format(time.RFC3339),
			"source":        "sdk",
		}
		out = append(out, provider.Node{
			ID:    p.ProjectId,
			Name:  p.DisplayName,
			Kind:  provider.KindProject,
			State: state,
			Meta:  meta,
		})
	}
	return out, true, nil
}

// searchAssetsSDK enumerates resources in a project via the Cloud
// Asset Inventory v1 SearchAllResources RPC. Returns
// (nil, false, err) when the SDK isn't available (no ADC, no
// CloudAsset API enablement) so the caller falls back to gcloud.
//
// assetTypes is the same comma-joined whitelist the gcloud path uses;
// pass "" to search every type.
func (g *GCP) searchAssetsSDK(ctx context.Context, project provider.Node, assetTypes string, limit int) ([]provider.Node, bool, error) {
	client, err := g.assetClient(ctx)
	if err != nil || client == nil {
		return nil, false, err
	}
	req := &assetpb.SearchAllResourcesRequest{
		Scope:    "projects/" + project.ID,
		PageSize: int32(limit),
	}
	if assetTypes != "" {
		req.AssetTypes = splitCSV(assetTypes)
	}
	it := client.SearchAllResources(ctx, req)
	out := make([]provider.Node, 0, limit)
	for len(out) < limit+1 {
		r, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, true, fmt.Errorf("gcp asset SDK: %w", err)
		}
		meta := map[string]string{
			"type":          r.AssetType,
			"project":       r.Project,
			"projectNumber": r.Project,
			"source":        "sdk",
		}
		if r.CreateTime != nil {
			meta["createTime"] = r.CreateTime.AsTime().Format(time.RFC3339)
		}
		if r.UpdateTime != nil {
			meta["updateTime"] = r.UpdateTime.AsTime().Format(time.RFC3339)
		}
		for k, v := range r.Labels {
			meta["label:"+k] = v
		}
		name := r.DisplayName
		if name == "" {
			name = lastSegment(r.Name)
		}
		out = append(out, provider.Node{
			ID:       r.Name,
			Name:     name,
			Kind:     provider.KindResource,
			Location: r.Location,
			Parent:   &project,
			Meta:     meta,
		})
	}
	return out, true, nil
}

// splitCSV splits a comma-separated list into trimmed entries.
func splitCSV(s string) []string {
	out := make([]string, 0, 16)
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			seg := s[start:i]
			// trim leading/trailing whitespace cheaply
			for len(seg) > 0 && (seg[0] == ' ' || seg[0] == '\t') {
				seg = seg[1:]
			}
			for len(seg) > 0 && (seg[len(seg)-1] == ' ' || seg[len(seg)-1] == '\t') {
				seg = seg[:len(seg)-1]
			}
			if seg != "" {
				out = append(out, seg)
			}
			start = i + 1
		}
	}
	return out
}

// lastSegment returns everything after the final '/' in a resource
// name. Used as the display fallback when the asset has no
// displayName populated (most non-compute resources).
func lastSegment(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '/' {
			return name[i+1:]
		}
	}
	return name
}

// parseProjectNumber pulls the numeric project number out of the
// fully-qualified resource name "projects/<NUM>". Returns "" on a
// malformed name; cloudnav surfaces project number for users who
// need it for IAM bindings, but the rest of the TUI keys off
// projectId.
func parseProjectNumber(resourceName string) string {
	const prefix = "projects/"
	if len(resourceName) <= len(prefix) {
		return ""
	}
	if resourceName[:len(prefix)] != prefix {
		return ""
	}
	return resourceName[len(prefix):]
}
