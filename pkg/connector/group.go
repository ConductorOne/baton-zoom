package connector

import (
	"context"
	"fmt"

	"github.com/ConductorOne/baton-zoom/pkg/zoom"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	grant "github.com/conductorone/baton-sdk/pkg/types/grant"
	resource "github.com/conductorone/baton-sdk/pkg/types/resource"
)

var entitlements = []string{
	memberEntitlement,
	adminEntitlement,
}

type groupResourceType struct {
	resourceType *v2.ResourceType
	client       *zoom.Client
}

func (g *groupResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return g.resourceType
}

// Create a new connector resource for a Zoom group.
func groupResource(group zoom.Group, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	profile := map[string]interface{}{
		"group_name": group.Name,
		"group_id":   group.ID,
	}

	groupTraitOptions := []resource.GroupTraitOption{
		resource.WithGroupProfile(profile),
	}

	ret, err := resource.NewGroupResource(
		group.Name,
		resourceTypeGroup,
		group.ID,
		groupTraitOptions,
		resource.WithParentResourceID(parentResourceID),
	)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func (g *groupResourceType) List(ctx context.Context, parentId *v2.ResourceId, token *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	var pageToken string
	var rv []*v2.Resource

	bag, page, err := parsePageToken(token.Token, &v2.ResourceId{ResourceType: resourceTypeGroup.Id})
	if err != nil {
		return nil, "", nil, err
	}

	groups, nextToken, resp, err := g.client.GetGroups(ctx, page)
	if err != nil {
		return nil, "", nil, err
	}
	resp.Body.Close()

	if nextToken != "" {
		pageToken, err = bag.NextToken(nextToken)
		if err != nil {
			return nil, "", nil, err
		}
	}

	annos, err := parseResp(resp)
	if err != nil {
		return nil, "", nil, err
	}

	for _, group := range groups {
		groupCopy := group
		gr, err := groupResource(groupCopy, parentId)
		if err != nil {
			return nil, "", nil, err
		}
		rv = append(rv, gr)
	}

	return rv, pageToken, annos, nil
}

func (g *groupResourceType) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var rv []*v2.Entitlement

	for _, entitlement := range entitlements {
		options := []ent.EntitlementOption{
			ent.WithGrantableTo(resourceTypeUser),
			ent.WithDescription(fmt.Sprintf("Zoom %s group", resource.DisplayName)),
			ent.WithDisplayName(fmt.Sprintf("%s group %s", resource.DisplayName, entitlement)),
		}
		en := ent.NewAssignmentEntitlement(resource, entitlement, options...)
		rv = append(rv, en)
	}
	return rv, "", nil, nil
}

func (g *groupResourceType) Grants(ctx context.Context, resource *v2.Resource, token *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	var rv []*v2.Grant

	groupMembers, err := g.client.GetGroupMembers(ctx, resource.Id.Resource)
	if err != nil {
		return nil, "", nil, err
	}

	for _, member := range groupMembers {
		memberCopy := member
		ur, err := userResource(memberCopy, resource.Id)
		if err != nil {
			return nil, "", nil, err
		}

		membershipGrant := grant.NewGrant(resource, memberEntitlement, ur.Id)
		rv = append(rv, membershipGrant)
	}

	groupAdmins, err := g.client.GetGroupAdmins(ctx, resource.Id.Resource)
	if err != nil {
		return nil, "", nil, err
	}

	for _, admin := range groupAdmins {
		adminCopy := admin
		ur, err := userResource(adminCopy, resource.Id)
		if err != nil {
			return nil, "", nil, err
		}

		adminGrant := grant.NewGrant(resource, adminEntitlement, ur.Id)
		rv = append(rv, adminGrant)
	}

	return rv, "", nil, nil
}

func groupBuilder(client *zoom.Client) *groupResourceType {
	return &groupResourceType{
		resourceType: resourceTypeGroup,
		client:       client,
	}
}
