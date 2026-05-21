package connector

import (
	"context"
	"fmt"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	resource "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-zoom/pkg/zoom"
)

const assignedEntitlement = "assigned"

type licenseResourceType struct {
	resourceType *v2.ResourceType
	client       *zoom.Client
}

func (l *licenseResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return l.resourceType
}

func licenseResource(id, displayName string, plan *zoom.Plan, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	entitlementID := fmt.Sprintf("%s:%s:%s", resourceTypeLicense.Id, id, assignedEntitlement)
	ret, err := resource.NewResource(
		displayName,
		resourceTypeLicense,
		id,
		resource.WithParentResourceID(parentResourceID),
		resource.WithLicenseProfileTrait(
			resource.WithLicenseName(displayName),
			resource.WithLicenseEntitlementIDs(entitlementID),
			resource.WithLicenseSeats(int64(plan.Hosts), int64(plan.Usage)),
		),
	)
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func (l *licenseResourceType) List(ctx context.Context, parentId *v2.ResourceId, _ resource.SyncOpAttrs) ([]*v2.Resource, *resource.SyncOpResults, error) {
	usage, resp, err := l.client.GetLicenses(ctx)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, nil, fmt.Errorf("baton-zoom: failed to list licenses: %w", err)
	}
	defer resp.Body.Close()

	annos, err := parseResp(resp)
	if err != nil {
		return nil, nil, err
	}

	named := []struct {
		id, name string
		plan     *zoom.Plan
	}{
		{"plan_base", "Plan Base", usage.PlanBase},
		{"plan_united", "Plan United", usage.PlanUnited},
		{"plan_zoom_rooms", "Plan Zoom Rooms", usage.PlanZoomRooms},
		{"plan_cloud_recording", "Plan Cloud Recording", usage.PlanCloudRecording},
		{"plan_audio", "Plan Audio", usage.PlanAudio},
		{"plan_recording", "Plan Recording", usage.PlanRecording},
	}

	var rv []*v2.Resource
	for _, n := range named {
		if n.plan == nil {
			continue
		}
		r, err := licenseResource(n.id, n.name, n.plan, parentId)
		if err != nil {
			return nil, nil, err
		}
		rv = append(rv, r)
	}
	for _, p := range usage.PlanLargeMeeting {
		if p == nil {
			continue
		}
		r, err := licenseResource(
			fmt.Sprintf("plan_large_meeting_%s", p.Type),
			fmt.Sprintf("Plan Large Meeting %s", p.Type),
			p, parentId,
		)
		if err != nil {
			return nil, nil, err
		}
		rv = append(rv, r)
	}
	for _, p := range usage.PlanWebinar {
		if p == nil {
			continue
		}
		r, err := licenseResource(
			fmt.Sprintf("plan_webinar_%s", p.Type),
			fmt.Sprintf("Plan Webinar %s", p.Type),
			p, parentId,
		)
		if err != nil {
			return nil, nil, err
		}
		rv = append(rv, r)
	}
	return rv, &resource.SyncOpResults{Annotations: annos}, nil
}

func (l *licenseResourceType) Entitlements(_ context.Context, r *v2.Resource, _ resource.SyncOpAttrs) ([]*v2.Entitlement, *resource.SyncOpResults, error) {
	options := []ent.EntitlementOption{
		ent.WithGrantableTo(resourceTypeUser),
		ent.WithDescription(fmt.Sprintf("%s Zoom license", r.DisplayName)),
		ent.WithDisplayName(fmt.Sprintf("%s license %s", r.DisplayName, assignedEntitlement)),
	}
	en := ent.NewAssignmentEntitlement(r, assignedEntitlement, options...)
	return []*v2.Entitlement{en}, &resource.SyncOpResults{}, nil
}

func (l *licenseResourceType) Grants(ctx context.Context, r *v2.Resource, opts resource.SyncOpAttrs) ([]*v2.Grant, *resource.SyncOpResults, error) {
	return nil, &resource.SyncOpResults{}, nil
}

func licenseBuilder(client *zoom.Client) *licenseResourceType {
	return &licenseResourceType{
		resourceType: resourceTypeLicense,
		client:       client,
	}
}
