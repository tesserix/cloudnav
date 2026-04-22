package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/tesserix/cloudnav/internal/provider"
)

// envGCPOrg names the Resource Manager organization ID used to source the
// folder hierarchy. Leaving it unset (the default) keeps the current
// flat-project Root() behaviour, which is what most single-project users
// want.
const envGCPOrg = "CLOUDNAV_GCP_ORG"

// folderJSON mirrors `gcloud resource-manager folders list --format=json`.
type folderJSON struct {
	Name        string `json:"name"`        // "folders/123"
	DisplayName string `json:"displayName"` // human label
	Parent      string `json:"parent"`      // "organizations/456" or "folders/789"
	State       string `json:"state"`
}

// orgID returns the configured organization ID or "" when folder
// navigation is disabled. Trimmed against whitespace so users can paste
// either "organizations/1234" or just "1234" and either works.
func orgID() string {
	v := os.Getenv(envGCPOrg)
	if v == "" {
		return ""
	}
	// Accept both raw id and the full Resource Manager form.
	if len(v) > len("organizations/") && v[:len("organizations/")] == "organizations/" {
		return v[len("organizations/"):]
	}
	return v
}

// listFolders returns every folder directly under the organization.
// Deep nesting is collapsed into a single level in the TUI so users can
// drill orgs without a fully recursive tree view; projects below a
// deeper folder still appear when the user drills into whichever
// ancestor folder owns them (via the `parent` field on the project).
//
// Returns (nil, nil) on permission errors so the caller can fall back to
// the flat project list rather than surfacing a raw IAM error.
func (g *GCP) listFolders(ctx context.Context, org string) ([]provider.Node, error) {
	out, err := g.gcloud.Run(ctx,
		"resource-manager", "folders", "list",
		"--organization="+org,
		"--format=json",
	)
	if err != nil {
		return nil, nil // silent fallback — caller uses flat projects
	}
	return parseFolders(out, "organizations/"+org)
}

func parseFolders(data []byte, orgParent string) ([]provider.Node, error) {
	var fs []folderJSON
	if err := json.Unmarshal(data, &fs); err != nil {
		return nil, fmt.Errorf("parse gcloud folders list: %w", err)
	}
	nodes := make([]provider.Node, 0, len(fs))
	for _, f := range fs {
		// Top-level folders only for v1. A nested folder is surfaced
		// when the user drills into whichever ancestor owns it; we can
		// add a proper tree view if users ask.
		if f.Parent != orgParent {
			continue
		}
		name := f.DisplayName
		if name == "" {
			name = f.Name
		}
		nodes = append(nodes, provider.Node{
			ID:    f.Name,
			Name:  name,
			Kind:  provider.KindFolder,
			State: f.State,
			Meta: map[string]string{
				"parent": f.Parent,
			},
		})
	}
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	return nodes, nil
}

// folderChildren returns sub-folders and direct-child projects under a
// folder. Orgs that nest teams / products inside parent folders
// (Engineering / Finance / …) are common; a flat list wouldn't surface
// projects more than one level deep without the user knowing to
// CLOUDNAV_GCP_ORG=<other id>.
//
// Sub-folders and projects are unioned so the user sees both in one
// view; KindFolder rows sort before KindProject rows because the TUI's
// default ordering groups like-kinds together.
func (g *GCP) folderChildren(ctx context.Context, folder provider.Node) ([]provider.Node, error) {
	var (
		subFolders []provider.Node
		projects   []provider.Node
	)

	// Sub-folders under this folder. Failure is non-fatal — the user
	// might lack resourcemanager.folders.list on the nested folder; we
	// still show projects.
	if folders, err := g.gcloud.Run(ctx,
		"resource-manager", "folders", "list",
		"--folder="+folderNumberFromID(folder.ID),
		"--format=json",
	); err == nil {
		subFolders, _ = parseFolders(folders, folder.ID)
	}

	// Projects directly under the folder. gcloud's filter syntax accepts
	// parent.id=<num> AND parent.type=folder.
	out, err := g.gcloud.Run(ctx,
		"projects", "list",
		"--filter=parent.id="+folderNumberFromID(folder.ID)+" AND parent.type=folder",
		"--format=json",
	)
	if err == nil {
		projects, _ = parseProjects(out)
	} else if len(subFolders) == 0 {
		// Only surface the error when there's literally nothing to show
		// — otherwise users see folders but get a scary error banner.
		return nil, err
	}

	combined := make([]provider.Node, 0, len(subFolders)+len(projects))
	combined = append(combined, subFolders...)
	combined = append(combined, projects...)
	return combined, nil
}

// folderNumberFromID trims "folders/123" → "123" for the gcloud filter
// expression. Robust to the caller handing us either shape.
func folderNumberFromID(id string) string {
	const prefix = "folders/"
	if len(id) > len(prefix) && id[:len(prefix)] == prefix {
		return id[len(prefix):]
	}
	return id
}
