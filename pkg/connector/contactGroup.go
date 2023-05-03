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

type contactGroupResourceType struct {
	resourceType *v2.ResourceType
	client       *zoom.Client
}

func (g *contactGroupResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return g.resourceType
}

// Create a new connector resource for a Zoom group.
func contactGroupResource(group zoom.ContactGroup, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	profile := map[string]interface{}{
		"group_name": group.Name,
		"group_id":   group.ID,
	}

	groupTraitOptions := []resource.GroupTraitOption{
		resource.WithGroupProfile(profile),
	}

	ret, err := resource.NewGroupResource(
		group.Name,
		resourceTypeContactGroup,
		group.ID,
		groupTraitOptions,
		resource.WithParentResourceID(parentResourceID),
	)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func (g *contactGroupResourceType) List(ctx context.Context, parentId *v2.ResourceId, token *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	var pageToken string
	var rv []*v2.Resource

	bag, page, err := parsePageToken(token.Token, &v2.ResourceId{ResourceType: resourceTypeContactGroup.Id})
	if err != nil {
		return nil, "", nil, err
	}

	groups, nextToken, resp, err := g.client.GetContactGroups(ctx, page)
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
		cgr, err := contactGroupResource(groupCopy, parentId)
		if err != nil {
			return nil, "", nil, err
		}
		rv = append(rv, cgr)
	}

	return rv, pageToken, annos, nil
}

func (g *contactGroupResourceType) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var rv []*v2.Entitlement

	membershipOptions := []ent.EntitlementOption{
		ent.WithGrantableTo(resourceTypeUser, resourceTypeGroup),
		ent.WithDescription(fmt.Sprintf("Zoom %s group", resource.DisplayName)),
		ent.WithDisplayName(fmt.Sprintf("%s group %s", resource.DisplayName, memberEntitlement)),
	}

	en := ent.NewAssignmentEntitlement(resource, memberEntitlement, membershipOptions...)
	rv = append(rv, en)

	return rv, "", nil, nil
}

func (g *contactGroupResourceType) Grants(ctx context.Context, resource *v2.Resource, token *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	var rv []*v2.Grant
	var pageToken string

	bag, page, err := parsePageToken(token.Token, &v2.ResourceId{ResourceType: resourceTypeContactGroup.Id})
	if err != nil {
		return nil, "", nil, err
	}

	groupMembers, nextToken, resp, err := g.client.GetContactGroupMembers(ctx, resource.Id.Resource, page)
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

	for _, member := range groupMembers {
		memberCopy := member
		// member type 1 is user, 2 is user group
		if member.Type == 1 {
			ur, err := userResource(zoom.User{
				ID:          memberCopy.ID,
				DisplayName: memberCopy.Name,
			}, resource.Id)
			if err != nil {
				return nil, "", nil, err
			}

			userGrant := grant.NewGrant(resource, memberEntitlement, ur.Id)
			rv = append(rv, userGrant)
		} else {
			gr, err := groupResource(zoom.Group{
				ID:   memberCopy.ID,
				Name: memberCopy.Name,
			}, resource.Id)
			if err != nil {
				return nil, "", nil, err
			}

			groupGrant := grant.NewGrant(resource, memberEntitlement, gr.Id)
			rv = append(rv, groupGrant)
		}
	}

	return rv, pageToken, annos, nil
}

func contactGroupBuilder(client *zoom.Client) *contactGroupResourceType {
	return &contactGroupResourceType{
		resourceType: resourceTypeContactGroup,
		client:       client,
	}
}
