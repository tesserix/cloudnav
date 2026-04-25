package aws

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tesserix/cloudnav/internal/provider"
)

type describeJSON struct {
	Reservations []struct {
		Instances []struct {
			InstanceID   string `json:"InstanceId"`
			InstanceType string `json:"InstanceType"`
			State        struct {
				Name string `json:"Name"`
			} `json:"State"`
			Placement struct {
				AvailabilityZone string `json:"AvailabilityZone"`
			} `json:"Placement"`
			Tags []struct {
				Key   string `json:"Key"`
				Value string `json:"Value"`
			} `json:"Tags"`
		} `json:"Instances"`
	} `json:"Reservations"`
}

func (a *AWS) ListVMs(ctx context.Context, scope provider.Node) ([]provider.VM, error) {
	if scope.Kind != provider.KindRegion {
		return nil, fmt.Errorf("aws: vm list expects region scope, got %q", scope.Kind)
	}
	// SDK fast path — ec2:DescribeInstances via the v2 SDK with
	// pagination.
	if vms, sdkUsable, err := a.listVMsSDK(ctx, scope.ID); sdkUsable && err == nil {
		return vms, nil
	}
	out, err := a.aws.Run(ctx,
		"ec2", "describe-instances",
		"--region", scope.ID,
		"--output", "json",
	)
	if err != nil {
		return nil, err
	}
	var env describeJSON
	if err := json.Unmarshal(out, &env); err != nil {
		return nil, fmt.Errorf("parse aws ec2 describe-instances: %w", err)
	}
	vms := []provider.VM{}
	for _, r := range env.Reservations {
		for _, i := range r.Instances {
			name := i.InstanceID
			for _, t := range i.Tags {
				if t.Key == "Name" && t.Value != "" {
					name = t.Value
					break
				}
			}
			vms = append(vms, provider.VM{
				ID:       i.InstanceID,
				Name:     name,
				State:    i.State.Name,
				Type:     i.InstanceType,
				Location: i.Placement.AvailabilityZone,
				Meta: map[string]string{
					"region": scope.ID,
					"zone":   i.Placement.AvailabilityZone,
				},
			})
		}
	}
	return vms, nil
}

func (a *AWS) ShowVM(ctx context.Context, id, region string) ([]byte, error) {
	if region == "" {
		return nil, fmt.Errorf("aws: vm show needs --region")
	}
	if data, sdkUsable, err := a.showVMSDK(ctx, id, region); sdkUsable && err == nil {
		return data, nil
	}
	return a.aws.Run(ctx, "ec2", "describe-instances",
		"--instance-ids", id, "--region", region, "--output", "json")
}

func (a *AWS) StartVM(ctx context.Context, id, region string) error {
	if region == "" {
		return fmt.Errorf("aws: vm start needs --region")
	}
	if sdkUsable, err := a.startVMSDK(ctx, id, region); sdkUsable {
		return err
	}
	_, err := a.aws.Run(ctx, "ec2", "start-instances",
		"--instance-ids", id, "--region", region, "--output", "json")
	return err
}

func (a *AWS) StopVM(ctx context.Context, id, region string) error {
	if region == "" {
		return fmt.Errorf("aws: vm stop needs --region")
	}
	if sdkUsable, err := a.stopVMSDK(ctx, id, region); sdkUsable {
		return err
	}
	_, err := a.aws.Run(ctx, "ec2", "stop-instances",
		"--instance-ids", id, "--region", region, "--output", "json")
	return err
}
