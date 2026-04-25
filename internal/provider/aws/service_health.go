package aws

import (
	"context"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/health"
	healthtypes "github.com/aws/aws-sdk-go-v2/service/health/types"

	"github.com/tesserix/cloudnav/internal/provider"
)

// AWS Health API client lifecycle. Note: Health is a paid feature
// (Business / Enterprise support plans only); free-tier accounts
// get NotAccessibleException. We treat that as "no events" rather
// than an error so the H overlay renders cleanly.
var (
	healthOnce    sync.Once
	healthClient  *health.Client
	healthInitErr error
)

func (a *AWS) healthClient(ctx context.Context) (*health.Client, error) {
	healthOnce.Do(func() {
		cfg, err := sdkConfig(ctx)
		if err != nil {
			healthInitErr = err
			return
		}
		healthClient = health.NewFromConfig(cfg)
	})
	return healthClient, healthInitErr
}

// HealthEvents satisfies provider.HealthEventer for AWS. Returns
// open events from the AWS Health API, normalised into the same
// provider.HealthEvent shape Azure / GCP use.
//
// AccessDenied / NotAccessibleException (caller doesn't have a paid
// support plan) maps to nil so the H overlay just shows an empty
// list rather than a scary error.
func (a *AWS) HealthEvents(ctx context.Context) ([]provider.HealthEvent, error) {
	client, err := a.healthClient(ctx)
	if err != nil || client == nil {
		return nil, nil
	}
	pager := health.NewDescribeEventsPaginator(client, &health.DescribeEventsInput{
		Filter: &healthtypes.EventFilter{
			EventStatusCodes: []healthtypes.EventStatusCode{
				healthtypes.EventStatusCodeOpen,
				healthtypes.EventStatusCodeUpcoming,
			},
		},
	})
	out := make([]provider.HealthEvent, 0, 8)
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			// Free-tier accounts get NotAccessibleException — quiet
			// degrade so the overlay still works for non-AWS clouds
			// in the same session.
			return out, nil
		}
		for _, ev := range page.Events {
			level := "advisory"
			switch ev.EventTypeCategory {
			case healthtypes.EventTypeCategoryIssue:
				level = "incident"
			case healthtypes.EventTypeCategoryScheduledChange:
				level = "maintenance"
			}
			startTime := ""
			if ev.StartTime != nil {
				startTime = ev.StartTime.Format(time.RFC3339)
			}
			out = append(out, provider.HealthEvent{
				ID:        aws.ToString(ev.Arn),
				Title:     aws.ToString(ev.EventTypeCode),
				Level:     level,
				Status:    string(ev.StatusCode),
				Service:   aws.ToString(ev.Service),
				Region:    aws.ToString(ev.Region),
				StartTime: startTime,
			})
		}
	}
	return out, nil
}

// Compile-time assert AWS satisfies HealthEventer.
var _ provider.HealthEventer = (*AWS)(nil)
