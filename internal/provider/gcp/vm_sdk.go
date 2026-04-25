package gcp

import (
	"context"
	"sync"
	"time"

	compute "cloud.google.com/go/compute/apiv1"
	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/tesserix/cloudnav/internal/provider"
)

// Compute SDK client lifecycle: lazily initialised package-level
// singletons. Kept package-scoped (rather than on g.sdk) so this
// file owns the entire Phase-3 surface and the next phase can
// follow the same pattern without colliding.
var (
	computeOnce    sync.Once
	computeClient  *compute.InstancesClient
	computeInitErr error
)

// instancesClient returns the process-shared Compute Engine
// InstancesClient. ADC-authenticated, single HTTP/2 connection,
// cached error on first failure (subsequent calls return the same
// error without re-paying the auth probe latency).
func (g *GCP) instancesClient(ctx context.Context) (*compute.InstancesClient, error) {
	computeOnce.Do(func() {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		client, err := compute.NewInstancesRESTClient(c)
		if err != nil {
			computeInitErr = err
			return
		}
		computeClient = client
	})
	return computeClient, computeInitErr
}

// listVMsSDK enumerates compute instances across every zone in a
// project via the AggregatedList RPC — one call instead of N
// per-zone calls. Returns (nil, false, err) when the SDK isn't
// usable so the caller falls back to gcloud.
func (g *GCP) listVMsSDK(ctx context.Context, project provider.Node) ([]provider.VM, bool, error) {
	client, err := g.instancesClient(ctx)
	if err != nil || client == nil {
		return nil, false, err
	}
	req := &computepb.AggregatedListInstancesRequest{
		Project: project.ID,
	}
	it := client.AggregatedList(ctx, req)
	out := make([]provider.VM, 0, 32)
	for {
		pair, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, true, err
		}
		// The aggregated map is keyed by "zones/<zone>"; per-zone
		// "Instances" carries the actual rows. Empty zones are
		// surfaced as warnings — skip them.
		if pair.Value == nil || pair.Value.Instances == nil {
			continue
		}
		zoneKey := pair.Key
		zone := lastSegment(zoneKey)
		for _, inst := range pair.Value.Instances {
			machineType := lastSegment(inst.GetMachineType())
			out = append(out, provider.VM{
				ID:       inst.GetName(),
				Name:     inst.GetName(),
				State:    inst.GetStatus(),
				Type:     machineType,
				Location: zone,
				Meta: map[string]string{
					"project": project.ID,
					"zone":    zone,
					"source":  "sdk",
				},
			})
		}
	}
	return out, true, nil
}

// startVMSDK powers on an instance via the SDK. Returns
// (false, err) when the SDK isn't usable so the caller can shell
// out to gcloud.
func (g *GCP) startVMSDK(ctx context.Context, id, project, zone string) (bool, error) {
	client, err := g.instancesClient(ctx)
	if err != nil || client == nil {
		return false, err
	}
	op, err := client.Start(ctx, &computepb.StartInstanceRequest{
		Project:  project,
		Zone:     zone,
		Instance: id,
	})
	if err != nil {
		return true, err
	}
	// Wait so the caller's "start succeeded" status is accurate
	// (matches the gcloud-cli behaviour the user is used to).
	if err := op.Wait(ctx); err != nil {
		return true, err
	}
	return true, nil
}

// stopVMSDK powers an instance off via the SDK. Same fall-through
// semantics as startVMSDK.
func (g *GCP) stopVMSDK(ctx context.Context, id, project, zone string) (bool, error) {
	client, err := g.instancesClient(ctx)
	if err != nil || client == nil {
		return false, err
	}
	op, err := client.Stop(ctx, &computepb.StopInstanceRequest{
		Project:  project,
		Zone:     zone,
		Instance: id,
	})
	if err != nil {
		return true, err
	}
	if err := op.Wait(ctx); err != nil {
		return true, err
	}
	return true, nil
}

// showVMSDK fetches the full instance descriptor as JSON bytes —
// matches the existing CLI path's output shape so the TUI's
// info-overlay JSON viewer stays unchanged.
func (g *GCP) showVMSDK(ctx context.Context, id, project, zone string) ([]byte, bool, error) {
	client, err := g.instancesClient(ctx)
	if err != nil || client == nil {
		return nil, false, err
	}
	inst, err := client.Get(ctx, &computepb.GetInstanceRequest{
		Project:  project,
		Zone:     zone,
		Instance: id,
	})
	if err != nil {
		return nil, true, err
	}
	// computepb.Instance implements json.Marshaler via the
	// protojson generator that ships with the SDK.
	data, err := jsonMarshalProto(inst)
	if err != nil {
		return nil, true, err
	}
	return data, true, nil
}

// closeComputeClient releases the process-shared Compute client.
// Called from g.Close() so the binary exits cleanly.
func closeComputeClient() error {
	if computeClient != nil {
		return computeClient.Close()
	}
	return nil
}

// jsonMarshalProto converts a proto message to indented JSON. Used
// so showVMSDK returns the same shape as the CLI fallback (which
// emits gcloud's --format=json output). protojson preserves field
// names from the .proto definitions; the TUI's info viewer doesn't
// care about wire vs JSON-name differences.
func jsonMarshalProto(m proto.Message) ([]byte, error) {
	opts := protojson.MarshalOptions{Indent: "  ", UseProtoNames: false}
	return opts.Marshal(m)
}

// AggregatedListInstances iterator pair type alias — the asset SDK
// already imports iterator above; we keep this here so a future
// reader sees both phases use the same pattern.
var _ = iterator.Done
