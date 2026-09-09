package connector

import (
	"context"
	"fmt"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	grant "github.com/conductorone/baton-sdk/pkg/types/grant"
	resource "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-zoom/pkg/zoom"
)

// roleExclusionGroup marks Zoom roles as mutually exclusive: a user carries
// exactly one role_id, so granting a role replaces the previous one.
const roleExclusionGroup = "zoom-role"

type roleResourceType struct {
	resourceType *v2.ResourceType
	client       *zoom.Client
}

func (r *roleResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return r.resourceType
}

func roleResource(role *zoom.Role, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	profile := map[string]any{
		"role_name": role.Name,
		"role_id":   role.ID,
	}
	return resource.NewRoleResource(
		role.Name,
		resourceTypeRole,
		role.ID,
		nil,
		resource.WithParentResourceID(parentResourceID),
		resource.WithResourceProfile(profile),
	)
}

func (r *roleResourceType) List(ctx context.Context, parentId *v2.ResourceId, _ resource.SyncOpAttrs) ([]*v2.Resource, *resource.SyncOpResults, error) {
	roles, annos, err := r.client.GetRoles(ctx)
	if err != nil {
		return nil, &resource.SyncOpResults{Annotations: annos}, fmt.Errorf("baton-zoom: list roles: %w", err)
	}

	rv := make([]*v2.Resource, 0, len(roles))
	for _, role := range roles {
		rr, err := roleResource(role, parentId)
		if err != nil {
			return nil, nil, err
		}
		rv = append(rv, rr)
	}

	return rv, &resource.SyncOpResults{Annotations: annos}, nil
}

func (r *roleResourceType) Entitlements(_ context.Context, res *v2.Resource, _ resource.SyncOpAttrs) ([]*v2.Entitlement, *resource.SyncOpResults, error) {
	roleOptions := []ent.EntitlementOption{
		ent.WithGrantableTo(resourceTypeUser),
		ent.WithExclusionGroup(roleExclusionGroup),
		ent.WithDescription(fmt.Sprintf("Role %s in zoom", res.DisplayName)),
		ent.WithDisplayName(fmt.Sprintf("%s role %s", res.DisplayName, memberEntitlement)),
	}
	return []*v2.Entitlement{
		ent.NewPermissionEntitlement(res, memberEntitlement, roleOptions...),
	}, &resource.SyncOpResults{}, nil
}

func (r *roleResourceType) Grants(_ context.Context, _ *v2.Resource, _ resource.SyncOpAttrs) ([]*v2.Grant, *resource.SyncOpResults, error) {
	return nil, nil, nil
}

func (r *roleResourceType) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) ([]*v2.Grant, annotations.Annotations, error) {
	if err := requireUserPrincipal(ctx, principal, "baton-zoom: only users can be granted role membership"); err != nil {
		return nil, nil, err
	}

	result := []*v2.Grant{
		grant.NewGrant(entitlement.GetResource(), entitlement.GetSlug(), principal.GetId()),
	}

	user, _, err := r.client.GetUser(ctx, principal.Id.Resource)
	if err != nil {
		return nil, nil, fmt.Errorf("baton-zoom: failed to get user before granting role: %w", err)
	}
	if user.Status == userStatusInactive {
		return nil, nil, fmt.Errorf("baton-zoom: cannot grant role to inactive user %s", principal.Id.Resource)
	}
	if user.RoleID == entitlement.Resource.Id.Resource {
		return result, annotations.New(&v2.GrantAlreadyExists{}), nil
	}

	var replacedGrantID string
	if user.RoleID != "" {
		previousRole := v2.Resource_builder{
			Id: v2.ResourceId_builder{
				ResourceType: resourceTypeRole.Id,
				Resource:     user.RoleID,
			}.Build(),
		}.Build()
		replacedGrantID = grant.NewGrant(previousRole, memberEntitlement, principal.GetId()).GetId()
	}

	err = r.client.AssignRole(ctx, entitlement.Resource.Id.Resource, principal.Id.Resource)
	if err != nil {
		return nil, nil, fmt.Errorf("baton-zoom: failed to assign role to user: %w", err)
	}

	var annos annotations.Annotations
	if replacedGrantID != "" {
		annos = grant.AppendGrantReplaced(annos, replacedGrantID)
	}
	return result, annos, nil
}

func (r *roleResourceType) Revoke(ctx context.Context, grant *v2.Grant) (annotations.Annotations, error) {
	entitlement := grant.Entitlement
	principal := grant.Principal
	if err := requireUserPrincipal(ctx, principal, "baton-zoom: only users can have role membership revoked"); err != nil {
		return nil, err
	}

	user, _, err := r.client.GetUser(ctx, principal.Id.Resource)
	if err != nil {
		return nil, fmt.Errorf("baton-zoom: failed to get user before revoking role: %w", err)
	}
	if user.Status == userStatusInactive {
		return nil, fmt.Errorf("baton-zoom: cannot revoke role from inactive user %s", principal.Id.Resource)
	}
	if user.RoleID != entitlement.Resource.Id.Resource {
		return annotations.New(&v2.GrantAlreadyRevoked{}), nil
	}

	err = r.client.UnassignRole(ctx, entitlement.Resource.Id.Resource, principal.Id.Resource)
	if err != nil {
		return nil, fmt.Errorf("baton-zoom: failed to unassign role from user: %w", err)
	}
	return nil, nil
}

func roleBuilder(client *zoom.Client) *roleResourceType {
	return &roleResourceType{
		resourceType: resourceTypeRole,
		client:       client,
	}
}
