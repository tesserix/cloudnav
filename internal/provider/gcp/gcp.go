// Package gcp implements provider.Provider by wrapping the `gcloud` CLI.
package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/tesserix/cloudnav/internal/cli"
	"github.com/tesserix/cloudnav/internal/provider"
)

type GCP struct {
	gcloud *cli.Runner
}

func New() *GCP {
	r := cli.New("gcloud")
	r.Timeout = 3 * time.Minute
	return &GCP{gcloud: r}
}

func (g *GCP) Name() string { return "gcp" }

func (g *GCP) LoggedIn(ctx context.Context) error {
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
		nodes = append(nodes, provider.Node{
			ID:    p.ProjectID,
			Name:  p.Name,
			Kind:  provider.KindProject,
			State: p.LifecycleState,
			Meta: map[string]string{
				"projectNumber": p.ProjectNumber,
				"createTime":    p.CreateTime,
			},
		})
	}
	return nodes, nil
}

func (g *GCP) Children(ctx context.Context, parent provider.Node) ([]provider.Node, error) {
	if parent.Kind != provider.KindProject {
		return nil, fmt.Errorf("gcp: no children for kind %q", parent.Kind)
	}
	return g.resources(ctx, parent)
}

type assetJSON struct {
	Name        string `json:"name"`
	AssetType   string `json:"assetType"`
	Location    string `json:"location"`
	DisplayName string `json:"displayName"`
	Project     string `json:"project"`
}

func (g *GCP) resources(ctx context.Context, project provider.Node) ([]provider.Node, error) {
	// Cloud Asset API requires the cloudasset.googleapis.com service enabled
	// on the caller's project. We fall back to a friendlier error when it's not.
	out, err := g.gcloud.Run(ctx,
		"asset", "search-all-resources",
		"--scope=projects/"+project.ID,
		"--format=json",
	)
	if err != nil {
		return nil, err
	}
	return parseAssets(out, project)
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
		nodes = append(nodes, provider.Node{
			ID:       a.Name,
			Name:     name,
			Kind:     provider.KindResource,
			Location: a.Location,
			State:    shortType(a.AssetType),
			Parent:   &parent,
			Meta: map[string]string{
				"type":    a.AssetType,
				"project": project.ID,
			},
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
