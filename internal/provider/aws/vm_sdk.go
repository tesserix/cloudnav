package aws

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"

	"github.com/tesserix/cloudnav/internal/provider"
)

// listVMsSDK enumerates EC2 instances in one region via
// ec2:DescribeInstances. Falls back to the CLI on auth failure
// or unsupported region.
func (a *AWS) listVMsSDK(ctx context.Context, region string) ([]provider.VM, bool, error) {
	client, err := a.ec2ClientForRegion(ctx, region)
	if err != nil || client == nil {
		return nil, false, err
	}
	pager := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{})
	vms := make([]provider.VM, 0, 16)
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, true, err
		}
		for _, r := range page.Reservations {
			for _, inst := range r.Instances {
				id := aws.ToString(inst.InstanceId)
				name := id
				for _, t := range inst.Tags {
					if aws.ToString(t.Key) == "Name" && aws.ToString(t.Value) != "" {
						name = aws.ToString(t.Value)
						break
					}
				}
				zone := ""
				if inst.Placement != nil {
					zone = aws.ToString(inst.Placement.AvailabilityZone)
				}
				vms = append(vms, provider.VM{
					ID:       id,
					Name:     name,
					State:    string(inst.State.Name),
					Type:     string(inst.InstanceType),
					Location: zone,
					Meta: map[string]string{
						"region": region,
						"zone":   zone,
						"source": "sdk",
					},
				})
			}
		}
	}
	return vms, true, nil
}

// showVMSDK returns the full instance descriptor as JSON bytes —
// matches the CLI fallback shape so the TUI's info-overlay JSON
// viewer doesn't need to know which path produced the data.
func (a *AWS) showVMSDK(ctx context.Context, id, region string) ([]byte, bool, error) {
	client, err := a.ec2ClientForRegion(ctx, region)
	if err != nil || client == nil {
		return nil, false, err
	}
	out, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{id},
	})
	if err != nil {
		return nil, true, err
	}
	// Marshal to JSON — keep the same envelope shape as the CLI
	// (`{"Reservations":[{"Instances":[...]}]}`) so callers parsing
	// the byte stream see no difference.
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, true, fmt.Errorf("marshal ec2 describe-instances: %w", err)
	}
	return data, true, nil
}

// startVMSDK powers an instance on. Returns (false, err) on SDK
// auth failure so the caller falls back to the CLI.
func (a *AWS) startVMSDK(ctx context.Context, id, region string) (bool, error) {
	client, err := a.ec2ClientForRegion(ctx, region)
	if err != nil || client == nil {
		return false, err
	}
	_, err = client.StartInstances(ctx, &ec2.StartInstancesInput{
		InstanceIds: []string{id},
	})
	return true, err
}

// stopVMSDK powers an instance off. Same shape as startVMSDK.
func (a *AWS) stopVMSDK(ctx context.Context, id, region string) (bool, error) {
	client, err := a.ec2ClientForRegion(ctx, region)
	if err != nil || client == nil {
		return false, err
	}
	_, err = client.StopInstances(ctx, &ec2.StopInstancesInput{
		InstanceIds: []string{id},
	})
	return true, err
}
