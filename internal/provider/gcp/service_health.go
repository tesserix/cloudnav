package gcp

import (
	"context"
	"sync"
	"time"

	servicehealth "cloud.google.com/go/servicehealth/apiv1"
	servicehealthpb "cloud.google.com/go/servicehealth/apiv1/servicehealthpb"
	"google.golang.org/api/iterator"

	"github.com/tesserix/cloudnav/internal/provider"
)

// Service Health SDK lifecycle. Personalized Service Health is the
// GCP analog of Azure's Microsoft.ResourceHealth feed and AWS
// Health API — surfaces incidents, planned maintenance, and
// advisories impacting the caller's accessible projects.
var (
	serviceHealthOnce    sync.Once
	serviceHealthClient  *servicehealth.Client
	serviceHealthInitErr error
)

func (g *GCP) serviceHealthClient(ctx context.Context) (*servicehealth.Client, error) {
	serviceHealthOnce.Do(func() {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		client, err := servicehealth.NewClient(c)
		if err != nil {
			serviceHealthInitErr = err
			return
		}
		serviceHealthClient = client
	})
	return serviceHealthClient, serviceHealthInitErr
}

// HealthEvents satisfies provider.HealthEventer. Iterates active
// incidents across every accessible project (Service Health is
// project-scoped) and normalises each into provider.HealthEvent so
// the H overlay renders identically across Azure / AWS / GCP.
//
// Errors fall through to (nil, nil) for individual projects so a
// single project without the API enabled doesn't break the whole
// overlay — it just contributes zero rows.
func (g *GCP) HealthEvents(ctx context.Context) ([]provider.HealthEvent, error) {
	client, err := g.serviceHealthClient(ctx)
	if err != nil || client == nil {
		// SDK unavailable — fall back to empty list rather than
		// dropping out to gcloud. Service Health doesn't have a
		// gcloud command at parity, so a quiet degrade is the
		// least-surprising option.
		return nil, nil
	}
	projectIDs, err := g.listProjectIDs(ctx)
	if err != nil || len(projectIDs) == 0 {
		return nil, err
	}

	out := make([]provider.HealthEvent, 0, 8)
	for _, pid := range projectIDs {
		// Personalized Service Health requires the location
		// "global" for project-scoped events.
		parent := "projects/" + pid + "/locations/global"
		it := client.ListEvents(ctx, &servicehealthpb.ListEventsRequest{
			Parent: parent,
		})
		for {
			ev, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				// API not enabled / permission denied / etc. —
				// quiet skip; users without a single project
				// configured for Service Health still see Azure
				// and AWS rows.
				break
			}
			out = append(out, healthEventFromPB(ev, pid))
		}
	}
	return out, nil
}

// healthEventFromPB normalises one Service Health event into the
// cross-cloud HealthEvent shape the H overlay renders.
func healthEventFromPB(ev *servicehealthpb.Event, projectID string) provider.HealthEvent {
	level := "advisory"
	if ev.GetCategory() == servicehealthpb.Event_INCIDENT {
		level = "incident"
	}
	status := "Active"
	if ev.GetState() == servicehealthpb.Event_CLOSED {
		status = "Resolved"
	}
	region := ""
	for _, loc := range ev.GetEventImpacts() {
		if loc != nil && loc.GetProduct() != nil && loc.GetLocation() != nil {
			region = loc.GetLocation().GetLocationName()
			break
		}
	}
	startTime := ""
	if t := ev.GetStartTime(); t != nil {
		startTime = t.AsTime().Format(time.RFC3339)
	}
	return provider.HealthEvent{
		ID:        ev.Name,
		Title:     ev.GetTitle(),
		Level:     level,
		Status:    status,
		Region:    region,
		Scope:     projectID,
		StartTime: startTime,
		Summary:   ev.GetDescription(),
	}
}

func closeServiceHealthClient() error {
	if serviceHealthClient != nil {
		return serviceHealthClient.Close()
	}
	return nil
}

// Compile-time assert GCP now implements HealthEventer at parity
// with Azure / AWS.
var _ provider.HealthEventer = (*GCP)(nil)
