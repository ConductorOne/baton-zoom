package connector

import (
	"context"
	"fmt"
	"net/http"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	grant "github.com/conductorone/baton-sdk/pkg/types/grant"
	resource "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-zoom/pkg/zoom"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type groupResourceType struct {
	resourceType *v2.ResourceType
	client       *zoom.Client
}

func (g *groupResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return g.resourceType
}

func groupResource(group *zoom.Group, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	profile := map[string]any{
		"group_name": group.Name,
		"group_id":   group.ID,
	}
	return resource.NewGroupResource(
		group.Name,
		resourceTypeGroup,
		group.ID,
		nil,
		resource.WithParentResourceID(parentResourceID),
		resource.WithResourceProfile(profile),
	)
}

func (g *groupResourceType) List(ctx context.Context, parentId *v2.ResourceId, opts resource.SyncOpAttrs) ([]*v2.Resource, *resource.SyncOpResults, error) {
	bag, page, err := parsePageToken(opts.PageToken.Token, &v2.ResourceId{ResourceType: resourceTypeGroup.Id}, "list groups")
	if err != nil {
		return nil, nil, err
	}

	groups, nextToken, annos, err := g.client.GetGroups(ctx, page)
	if err != nil {
		return nil, &resource.SyncOpResults{Annotations: annos}, fmt.Errorf("baton-zoom: list groups: %w", err)
	}

	pageToken, err := nextBagToken(bag, nextToken)
	if err != nil {
		return nil, nil, err
	}

	rv := make([]*v2.Resource, 0, len(groups))
	for _, group := range groups {
		gr, err := groupResource(group, parentId)
		if err != nil {
			return nil, nil, err
		}
		rv = append(rv, gr)
	}

	return rv, &resource.SyncOpResults{NextPageToken: pageToken, Annotations: annos}, nil
}

func (g *groupResourceType) Entitlements(_ context.Context, r *v2.Resource, _ resource.SyncOpAttrs) ([]*v2.Entitlement, *resource.SyncOpResults, error) {
	rv := make([]*v2.Entitlement, 0, 2)
	for _, entitlement := range []string{memberEntitlement, adminEntitlement} {
		options := []ent.EntitlementOption{
			ent.WithGrantableTo(resourceTypeUser),
			ent.WithDescription(fmt.Sprintf("Zoom %s group", r.DisplayName)),
			ent.WithDisplayName(fmt.Sprintf("%s group %s", r.DisplayName, entitlement)),
		}
		rv = append(rv, ent.NewAssignmentEntitlement(r, entitlement, options...))
	}
	return rv, &resource.SyncOpResults{}, nil
}

func (g *groupResourceType) Grants(ctx context.Context, r *v2.Resource, opts resource.SyncOpAttrs) ([]*v2.Grant, *resource.SyncOpResults, error) {
	bag, page, err := parsePageToken(opts.PageToken.Token, &v2.ResourceId{
		ResourceType: resourceTypeGroup.Id,
		Resource:     r.Id.Resource,
	}, "list group administrators")
	if err != nil {
		return nil, nil, err
	}

	admins, nextToken, annos, err := g.client.GetGroupAdmins(ctx, r.Id.Resource, page)
	if err != nil {
		return nil, &resource.SyncOpResults{Annotations: annos}, fmt.Errorf(
			"baton-zoom: list administrators for group %s: %w",
			r.Id.Resource,
			err,
		)
	}

	pageToken, err := nextBagToken(bag, nextToken)
	if err != nil {
		return nil, nil, err
	}

	rv := make([]*v2.Grant, 0, len(admins))
	for _, admin := range admins {
		ur, err := userResource(admin, r.Id)
		if err != nil {
			return nil, nil, err
		}
		rv = append(rv, grant.NewGrant(r, adminEntitlement, ur.Id))
	}

	return rv, &resource.SyncOpResults{NextPageToken: pageToken, Annotations: annos}, nil
}

func (g *groupResourceType) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) ([]*v2.Grant, annotations.Annotations, error) {
	if err := requireUserPrincipal(ctx, principal, "baton-zoom: only users can be granted group membership"); err != nil {
		return nil, nil, err
	}

	slug, err := groupEntitlementSlug(entitlement.GetId())
	if err != nil {
		return nil, nil, status.Errorf(codes.InvalidArgument, "baton-zoom: %v", err)
	}

	grants := []*v2.Grant{
		grant.NewGrant(entitlement.GetResource(), entitlement.GetSlug(), principal.GetId()),
	}
	groupID := entitlement.Resource.Id.Resource
	userID := principal.Id.Resource

	var (
		created bool
		annos   annotations.Annotations
	)
	switch slug {
	case memberEntitlement:
		created, annos, err = g.client.EnsureGroupMember(ctx, groupID, userID)
		if err != nil {
			return nil, annos, fmt.Errorf("baton-zoom: failed to add user to group: %w", err)
		}
	case adminEntitlement:
		created, annos, err = g.client.EnsureGroupAdmin(ctx, groupID, userID, primaryEmail(principal))
		if err != nil {
			return nil, annos, fmt.Errorf("baton-zoom: failed to add admin to group: %w", err)
		}
	}
	if !created {
		annos.Update(&v2.GrantAlreadyExists{})
	}
	return grants, annos, nil
}

func (g *groupResourceType) Revoke(ctx context.Context, grant *v2.Grant) (annotations.Annotations, error) {
	entitlement := grant.Entitlement
	principal := grant.Principal
	if err := requireUserPrincipal(ctx, principal, "baton-zoom: only users can have group membership revoked"); err != nil {
		return nil, err
	}

	slug, err := groupEntitlementSlug(entitlement.GetId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "baton-zoom: %v", err)
	}

	if slug == memberEntitlement {
		err := g.client.DeleteGroupMember(ctx, entitlement.Resource.Id.Resource, principal.Id.Resource)
		if err != nil {
			if zoom.IsAPIError(err, http.StatusNotFound, zoom.GroupMemberNotFoundErrorCode) ||
				zoom.IsAPIError(err, http.StatusNotFound, zoom.UserNotFoundErrorCode) {
				return annotations.New(&v2.GrantAlreadyRevoked{}), nil
			}
			return nil, fmt.Errorf("baton-zoom: failed to remove group member: %w", err)
		}
		return nil, nil
	}

	err = g.client.DeleteGroupAdmin(ctx, entitlement.Resource.Id.Resource, principal.Id.Resource)
	if err != nil {
		if zoom.IsAPIError(err, http.StatusBadRequest, zoom.GroupAdminNotFoundErrorCode) ||
			zoom.IsAPIError(err, http.StatusNotFound, zoom.UserNotFoundErrorCode) {
			return annotations.New(&v2.GrantAlreadyRevoked{}), nil
		}
		return nil, fmt.Errorf("baton-zoom: failed to remove group admin: %w", err)
	}
	return nil, nil
}

func groupBuilder(client *zoom.Client) *groupResourceType {
	return &groupResourceType{
		resourceType: resourceTypeGroup,
		client:       client,
	}
}
