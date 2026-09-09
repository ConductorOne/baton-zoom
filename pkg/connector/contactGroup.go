package connector

import (
	"context"
	"fmt"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	grant "github.com/conductorone/baton-sdk/pkg/types/grant"
	resource "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-zoom/pkg/zoom"
)

type contactGroupResourceType struct {
	resourceType *v2.ResourceType
	client       *zoom.Client
}

func (g *contactGroupResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return g.resourceType
}

// Create a new connector resource for a Zoom group.
func contactGroupResource(group *zoom.ContactGroup, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	profile := map[string]any{
		"group_name": group.Name,
		"group_id":   group.ID,
	}

	ret, err := resource.NewGroupResource(
		group.Name,
		resourceTypeContactGroup,
		group.ID,
		nil,
		resource.WithParentResourceID(parentResourceID),
		resource.WithResourceProfile(profile),
	)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func (g *contactGroupResourceType) List(ctx context.Context, parentId *v2.ResourceId, opts resource.SyncOpAttrs) ([]*v2.Resource, *resource.SyncOpResults, error) {
	bag, page, err := parsePageToken(opts.PageToken.Token, &v2.ResourceId{ResourceType: resourceTypeContactGroup.Id})
	if err != nil {
		return nil, nil, err
	}

	groups, nextToken, annos, err := g.client.GetContactGroups(ctx, page)
	if err != nil {
		return nil, nil, err
	}

	pageToken, err := nextBagToken(bag, nextToken)
	if err != nil {
		return nil, nil, err
	}

	rv := make([]*v2.Resource, 0, len(groups))
	for _, group := range groups {
		cgr, err := contactGroupResource(group, parentId)
		if err != nil {
			return nil, nil, err
		}
		rv = append(rv, cgr)
	}

	return rv, &resource.SyncOpResults{NextPageToken: pageToken, Annotations: annos}, nil
}

func (g *contactGroupResourceType) Entitlements(_ context.Context, r *v2.Resource, _ resource.SyncOpAttrs) ([]*v2.Entitlement, *resource.SyncOpResults, error) {
	membershipOptions := []ent.EntitlementOption{
		ent.WithDescription(fmt.Sprintf("Zoom %s group", r.DisplayName)),
		ent.WithDisplayName(fmt.Sprintf("%s group %s", r.DisplayName, memberEntitlement)),
	}
	return []*v2.Entitlement{
		ent.NewAssignmentEntitlement(r, memberEntitlement, membershipOptions...),
	}, &resource.SyncOpResults{}, nil
}

func (g *contactGroupResourceType) Grants(ctx context.Context, r *v2.Resource, opts resource.SyncOpAttrs) ([]*v2.Grant, *resource.SyncOpResults, error) {
	bag, page, err := parsePageToken(opts.PageToken.Token, &v2.ResourceId{ResourceType: resourceTypeContactGroup.Id})
	if err != nil {
		return nil, nil, err
	}

	groupMembers, nextToken, annos, err := g.client.GetContactGroupMembers(ctx, r.Id.Resource, page)
	if err != nil {
		return nil, nil, err
	}

	pageToken, err := nextBagToken(bag, nextToken)
	if err != nil {
		return nil, nil, err
	}

	rv := make([]*v2.Grant, 0, len(groupMembers))
	for _, member := range groupMembers {
		if member.Type == contactMemberTypeUser {
			ur, err := userResource(&zoom.User{
				ID:          member.ID,
				DisplayName: member.Name,
			}, r.Id)
			if err != nil {
				return nil, nil, err
			}
			rv = append(rv, grant.NewGrant(r, memberEntitlement, ur.Id))
			continue
		}

		gr, err := groupResource(&zoom.Group{
			ID:   member.ID,
			Name: member.Name,
		}, r.Id)
		if err != nil {
			return nil, nil, err
		}
		rv = append(rv, grant.NewGrant(
			r,
			memberEntitlement,
			gr.Id,
			grant.WithAnnotation(
				&v2.GrantExpandable{
					EntitlementIds:  []string{ent.NewEntitlementID(gr, memberEntitlement)},
					Shallow:         true,
					ResourceTypeIds: []string{resourceTypeUser.Id},
				},
				&v2.GrantImmutable{},
			),
		))
	}

	return rv, &resource.SyncOpResults{NextPageToken: pageToken, Annotations: annos}, nil
}

func contactGroupBuilder(client *zoom.Client) *contactGroupResourceType {
	return &contactGroupResourceType{
		resourceType: resourceTypeContactGroup,
		client:       client,
	}
}
