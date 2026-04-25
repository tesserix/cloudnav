package gcp

import (
	"context"
	"sync"
	"time"

	pam "cloud.google.com/go/privilegedaccessmanager/apiv1"
	pampb "cloud.google.com/go/privilegedaccessmanager/apiv1/privilegedaccessmanagerpb"
	"google.golang.org/api/iterator"

	"github.com/tesserix/cloudnav/internal/provider"
)

// PAM SDK lifecycle.
var (
	pamOnce    sync.Once
	pamClient  *pam.Client
	pamInitErr error
)

func (g *GCP) pamSDKClient(ctx context.Context) (*pam.Client, error) {
	pamOnce.Do(func() {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		client, err := pam.NewClient(c)
		if err != nil {
			pamInitErr = err
			return
		}
		pamClient = client
	})
	return pamClient, pamInitErr
}

// listProjectIDsSDK walks the SearchProjects iterator and returns
// every accessible project id. Replaces the gcloud projects list
// shell on the PIM hot path. Falls through to gcloud when ADC
// isn't usable.
func (g *GCP) listProjectIDsSDK(ctx context.Context) ([]string, bool, error) {
	nodes, sdkUsable, err := g.listProjectsSDK(ctx)
	if !sdkUsable || err != nil {
		return nil, sdkUsable, err
	}
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n.ID != "" {
			out = append(out, n.ID)
		}
	}
	return out, true, nil
}

// fetchPAMEntitlementsSDK queries Privileged Access Manager via the
// SDK. Returns (nil, nil, false, err) on SDK-unusable envs so the
// caller falls through to gcloud beta pam entitlements list.
func (g *GCP) fetchPAMEntitlementsSDK(ctx context.Context, projectID string) ([]provider.PIMRole, map[string]string, bool, error) {
	client, err := g.pamSDKClient(ctx)
	if err != nil || client == nil {
		return nil, nil, false, err
	}
	parent := "projects/" + projectID + "/locations/global"
	it := client.ListEntitlements(ctx, &pampb.ListEntitlementsRequest{
		Parent: parent,
	})
	roles := make([]provider.PIMRole, 0, 4)
	for {
		e, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			// Same shape as the CLI fallback: API-not-enabled is
			// silent so projects without PAM don't spam errors.
			return nil, nil, true, err
		}
		// PAM entitlements bind one or more roles. We surface the
		// entitlement display name + first role binding for the
		// PIM overlay row; the user activates by name.
		var roleName string
		if pb := e.GetPrivilegedAccess().GetGcpIamAccess(); pb != nil {
			for _, b := range pb.RoleBindings {
				if b != nil && b.Role != "" {
					roleName = b.Role
					break
				}
			}
		}
		roles = append(roles, provider.PIMRole{
			ID:        e.Name,
			RoleName:  roleName,
			Scope:     projectID,
			ScopeName: projectID,
			Source:    "gcp-pam",
		})
	}

	// Best-effort grants pass — separate call, separate iterator.
	grantsIt := client.ListGrants(ctx, &pampb.ListGrantsRequest{
		Parent: parent,
	})
	active := map[string]string{}
	for {
		gr, err := grantsIt.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		if gr.GetState() == pampb.Grant_ACTIVE {
			// Grant timeline events carry scheduled start/end —
			// the proto shape varies across SDK versions, so we
			// use protojson conservatively here: the timeline is
			// optional metadata, not a guarantee.
			until := ""
			// PAM grant.name = "...entitlements/<eid>/grants/<gid>"; we key on entitlement.
			eid := entitlementFromGrantName(gr.Name)
			if eid != "" {
				active["projects/"+projectID+"/locations/global/entitlements/"+eid] = until
			}
		}
	}
	return roles, active, true, nil
}

// entitlementFromGrantName pulls the entitlement id out of a grant
// resource name. "projects/p/locations/global/entitlements/<eid>/grants/<gid>".
func entitlementFromGrantName(name string) string {
	const before = "/entitlements/"
	const after = "/grants/"
	i := lastIndex(name, before)
	if i < 0 {
		return ""
	}
	rest := name[i+len(before):]
	j := indexOf(rest, after)
	if j < 0 {
		return rest
	}
	return rest[:j]
}

// Tiny string helpers — go's strings.LastIndex / strings.Index would
// work, but they pull a 200-line import for two callers.
func lastIndex(s, sep string) int {
	for i := len(s) - len(sep); i >= 0; i-- {
		if s[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}

func indexOf(s, sep string) int {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}

func closePAMClient() error {
	if pamClient != nil {
		return pamClient.Close()
	}
	return nil
}
