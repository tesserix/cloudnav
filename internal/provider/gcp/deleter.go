package gcp

import (
	"context"
	"fmt"
	"strings"
	"sync"

	compute "cloud.google.com/go/compute/apiv1"
	computepb "cloud.google.com/go/compute/apiv1/computepb"
	resourcemanagerpb "cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	"cloud.google.com/go/storage"

	"github.com/tesserix/cloudnav/internal/provider"
)

// Storage client lifecycle (used only by the deleter for bucket
// removal — kept here so this file owns its surface).
var (
	storageOnce    sync.Once
	storageClient  *storage.Client
	storageInitErr error
)

func (g *GCP) storageClient(ctx context.Context) (*storage.Client, error) {
	storageOnce.Do(func() {
		client, err := storage.NewClient(ctx)
		if err != nil {
			storageInitErr = err
			return
		}
		storageClient = client
	})
	return storageClient, storageInitErr
}

// Delete satisfies provider.Deleter for GCP. Each cloud asset type
// has its own delete RPC — there's no single ARM-style endpoint —
// so we dispatch on the asset type stored in Meta["type"]. Anything
// not in the table returns ErrNotSupported so the TUI surfaces a
// portal hand-off hint.
//
// Resource Manager / projects/{id} flows through DeleteProject, which
// itself respects any liens placed by the L overlay.
func (g *GCP) Delete(ctx context.Context, n provider.Node) error {
	switch n.Kind {
	case provider.KindProject:
		return g.deleteProject(ctx, n.ID)
	case provider.KindResource:
		// fall through to per-type dispatch below
	default:
		return fmt.Errorf("%w: gcp cannot delete kind %q", provider.ErrNotSupported, n.Kind)
	}

	assetType := n.Meta["type"]
	switch assetType {
	case "compute.googleapis.com/Instance":
		return g.deleteComputeInstance(ctx, n)
	case "storage.googleapis.com/Bucket":
		return g.deleteStorageBucket(ctx, n)
	default:
		return fmt.Errorf("%w: gcp delete for type %q (open the portal to delete this resource)",
			provider.ErrNotSupported, assetType)
	}
}

// deleteProject calls Resource Manager v3 DeleteProject. The RPC
// fails-fast when a lien is present — that's the lock-equivalent
// surfaced from the L overlay (Phase 7).
func (g *GCP) deleteProject(ctx context.Context, projectID string) error {
	client, err := g.projectsClient(ctx)
	if err != nil || client == nil {
		// Fall back to gcloud when the SDK isn't usable.
		_, err := g.gcloud.Run(ctx, "projects", "delete", projectID, "--quiet")
		return err
	}
	op, err := client.DeleteProject(ctx, &resourcemanagerpb.DeleteProjectRequest{
		Name: "projects/" + projectID,
	})
	if err != nil {
		return err
	}
	if _, err := op.Wait(ctx); err != nil {
		return err
	}
	return nil
}

// deleteComputeInstance fires Compute Engine's Instances.Delete on
// the zone parsed from the asset name. Falls back to gcloud on SDK
// unavailability.
func (g *GCP) deleteComputeInstance(ctx context.Context, n provider.Node) error {
	project := n.Meta["project"]
	if project == "" {
		project = nodeProjectFromID(n.ID)
	}
	zone := zoneFromInstanceID(n.ID)
	if project == "" || zone == "" {
		return fmt.Errorf("gcp delete instance: missing project / zone (id=%s)", n.ID)
	}
	client, err := g.instancesClient(ctx)
	if err != nil || client == nil {
		_, err := g.gcloud.Run(ctx,
			"compute", "instances", "delete", n.Name,
			"--project", project,
			"--zone", zone,
			"--quiet",
		)
		return err
	}
	op, err := client.Delete(ctx, &computepb.DeleteInstanceRequest{
		Project:  project,
		Zone:     zone,
		Instance: n.Name,
	})
	if err != nil {
		return err
	}
	return op.Wait(ctx)
}

// deleteStorageBucket calls Storage's Bucket.Delete. Empty-bucket
// requirement is GCP's, not ours — the SDK returns a typed error
// the user can act on.
func (g *GCP) deleteStorageBucket(ctx context.Context, n provider.Node) error {
	client, err := g.storageClient(ctx)
	if err != nil || client == nil {
		_, err := g.gcloud.Run(ctx, "storage", "rm", "--recursive", "gs://"+n.Name)
		return err
	}
	return client.Bucket(n.Name).Delete(ctx)
}

// zoneFromInstanceID pulls the zone segment out of a fully-qualified
// compute instance asset name:
//
//	//compute.googleapis.com/projects/<p>/zones/<zone>/instances/<name>
//
// Returns "" when the segment isn't present so the caller can fail
// with a clear "missing zone" error.
func zoneFromInstanceID(name string) string {
	const marker = "/zones/"
	i := strings.Index(name, marker)
	if i < 0 {
		return ""
	}
	rest := name[i+len(marker):]
	if j := strings.Index(rest, "/"); j >= 0 {
		return rest[:j]
	}
	return rest
}

// nodeProjectFromID extracts the project segment from a compute /
// storage asset id. Used as a fallback when Meta["project"] isn't
// populated (some asset rows from the gcloud-CLI fallback path).
//
// Accepts both forms the asset API returns:
//
//	//compute.googleapis.com/projects/<p>/zones/...   ← Asset Inventory
//	projects/<p>/zones/...                            ← gcloud CLI
func nodeProjectFromID(id string) string {
	const marker = "projects/"
	idx := strings.Index(id, marker)
	if idx < 0 {
		return ""
	}
	rest := id[idx+len(marker):]
	if j := strings.Index(rest, "/"); j >= 0 {
		return rest[:j]
	}
	return rest
}

func closeStorageClient() error {
	if storageClient != nil {
		return storageClient.Close()
	}
	return nil
}

// Compile-time check that GCP satisfies the Deleter interface.
var (
	_ provider.Deleter = (*GCP)(nil)
	_                  = compute.NewInstancesRESTClient // keep import alive even when guarded
)
