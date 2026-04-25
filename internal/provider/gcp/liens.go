package gcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tesserix/cloudnav/internal/provider"
)

// Liens are GCP's analog to Azure management locks. A lien on a
// project blocks any RPC carrying the named restricted permission
// — the canonical use is "cloudresourcemanager.projects.delete",
// which makes the project undeletable until the lien is removed.
//
// Liens are project-scoped (not resource-scoped), so cloudnav only
// surfaces them on the L overlay when the user is at the project
// row. Resources inside a project don't carry their own liens.
//
// Implementation note: Liens live on the v1 Resource Manager API
// surface, which doesn't have a Go SDK client in the
// cloud.google.com/go/resourcemanager v3 module. Rather than pull
// in google.golang.org/api/cloudresourcemanager/v1 just for this
// one RPC, we use `gcloud alpha resource-manager liens ...` —
// auth-shared with the active gcloud config, no extra deps. The
// performance hit (one subprocess per L overlay open) is fine
// because the user only opens this overlay manually and rarely.
//
// Reference: cloud.google.com/resource-manager/reference/rest/v3/liens

// Locks satisfies provider.Locker for GCP. Lists liens bound to the
// project. Empty slice when the project has none. Returns nil for
// non-project nodes — liens don't apply to individual resources.
func (g *GCP) Locks(ctx context.Context, n provider.Node) ([]provider.Lock, error) {
	if n.Kind != provider.KindProject {
		return nil, nil
	}
	out, err := g.gcloud.Run(ctx,
		"alpha", "resource-manager", "liens", "list",
		"--project="+n.ID,
		"--format=json",
	)
	if err != nil {
		return nil, err
	}
	return parseLiensCLI(out)
}

// CreateLock satisfies provider.Locker — places a lien blocking
// the canonical "cannot delete" permission on the project. Reason
// is surfaced back to whoever next tries to delete the project, so
// keep it human-readable ("locked by ops, do not delete" etc.).
func (g *GCP) CreateLock(ctx context.Context, n provider.Node, reason string) error {
	if n.Kind != provider.KindProject {
		return fmt.Errorf("%w: gcp liens are project-scoped (got kind %q)",
			provider.ErrNotSupported, n.Kind)
	}
	if reason == "" {
		reason = "locked by cloudnav"
	}
	_, err := g.gcloud.Run(ctx,
		"alpha", "resource-manager", "liens", "create",
		"--project="+n.ID,
		"--reason="+reason,
		"--restrictions=resourcemanager.projects.delete",
		"--origin=cloudnav",
	)
	return err
}

// RemoveLock satisfies provider.Locker — drops a lien by its
// resource name (returned in Locks() output as l.Name). Idempotent:
// removing a missing lien returns no error so the TUI can
// "remove all" without checking presence first.
func (g *GCP) RemoveLock(ctx context.Context, n provider.Node, name string) error {
	if n.Kind != provider.KindProject {
		return nil
	}
	_, err := g.gcloud.Run(ctx,
		"alpha", "resource-manager", "liens", "delete", name,
	)
	return err
}

func parseLiensCLI(data []byte) ([]provider.Lock, error) {
	var rows []struct {
		Name         string   `json:"name"`
		Reason       string   `json:"reason"`
		Origin       string   `json:"origin"`
		Restrictions []string `json:"restrictions"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, err
	}
	out := make([]provider.Lock, 0, len(rows))
	for _, r := range rows {
		out = append(out, provider.Lock{
			Name:   r.Name,
			Level:  "Lien",
			Reason: r.Reason,
			Origin: r.Origin,
		})
	}
	return out, nil
}

// Compile-time assert GCP implements Locker.
var _ provider.Locker = (*GCP)(nil)
