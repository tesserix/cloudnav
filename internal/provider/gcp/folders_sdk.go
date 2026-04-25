package gcp

import (
	"context"
	"sort"
	"sync"
	"time"

	resourcemanager "cloud.google.com/go/resourcemanager/apiv3"
	resourcemanagerpb "cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	"google.golang.org/api/iterator"

	"github.com/tesserix/cloudnav/internal/provider"
)

// Folders SDK lifecycle — kept package-scoped for the same reason as
// the other Phase-N files. v3 FoldersClient lists / searches via
// the same Cloud Resource Manager API the projects client uses,
// just under a different gRPC service.
var (
	foldersOnce    sync.Once
	foldersClient  *resourcemanager.FoldersClient
	foldersInitErr error
)

func (g *GCP) foldersSDKClient(ctx context.Context) (*resourcemanager.FoldersClient, error) {
	foldersOnce.Do(func() {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		client, err := resourcemanager.NewFoldersClient(c)
		if err != nil {
			foldersInitErr = err
			return
		}
		foldersClient = client
	})
	return foldersClient, foldersInitErr
}

// listFoldersSDK enumerates folders directly under a parent
// (organization or another folder) via Resource Manager v3
// ListFolders. Returns (nil, false, err) when the SDK isn't
// usable so the caller falls back to gcloud.
func (g *GCP) listFoldersSDK(ctx context.Context, parent string) ([]provider.Node, bool, error) {
	client, err := g.foldersSDKClient(ctx)
	if err != nil || client == nil {
		return nil, false, err
	}
	it := client.ListFolders(ctx, &resourcemanagerpb.ListFoldersRequest{
		Parent: parent,
	})
	out := make([]provider.Node, 0, 8)
	for {
		f, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, true, err
		}
		name := f.DisplayName
		if name == "" {
			name = f.Name
		}
		out = append(out, provider.Node{
			ID:    f.Name,
			Name:  name,
			Kind:  provider.KindFolder,
			State: f.State.String(),
			Meta: map[string]string{
				"parent": f.Parent,
				"source": "sdk",
			},
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, true, nil
}

// searchProjectsUnderFolderSDK returns every project whose parent is
// the given folder. Uses SearchProjects with a parent: filter rather
// than ListProjects(parent=…) because the latter only returns
// active projects and skips DELETE_REQUESTED ones — surfacing
// pending-delete projects is useful so the user can either undelete
// or finish the cleanup.
func (g *GCP) searchProjectsUnderFolderSDK(ctx context.Context, folderID string) ([]provider.Node, bool, error) {
	client, err := g.projectsClient(ctx)
	if err != nil || client == nil {
		return nil, false, err
	}
	it := client.SearchProjects(ctx, &resourcemanagerpb.SearchProjectsRequest{
		Query: "parent:" + folderID,
	})
	out := make([]provider.Node, 0, 8)
	for {
		p, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, true, err
		}
		state := p.State.String()
		out = append(out, provider.Node{
			ID:    p.ProjectId,
			Name:  p.DisplayName,
			Kind:  provider.KindProject,
			State: state,
			Meta: map[string]string{
				"projectNumber": parseProjectNumber(p.Name),
				"createTime":    p.CreateTime.AsTime().Format(time.RFC3339),
				"createdTime":   p.CreateTime.AsTime().Format(time.RFC3339),
				"source":        "sdk",
			},
		})
	}
	return out, true, nil
}

func closeFoldersClient() error {
	if foldersClient != nil {
		return foldersClient.Close()
	}
	return nil
}
