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
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

const (
	memberEntitlement = "member"
	adminEntitlement  = "admin"
)

type roleResourceType struct {
	resourceType *v2.ResourceType
	client       *zoom.Client
}

func (r *roleResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return r.resourceType
}

// Create a new connector resource for a Zoom role.
func roleResource(role zoom.Role, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	profile := map[string]any{
		"role_name": role.Name,
		"role_id":   role.ID,
	}

	ret, err := resource.NewRoleResource(
		role.Name,
		resourceTypeRole,
		role.ID,
		nil,
		resource.WithParentResourceID(parentResourceID),
		resource.WithResourceProfile(profile),
	)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func (r *roleResourceType) List(ctx context.Context, parentId *v2.ResourceId, _ resource.SyncOpAttrs) ([]*v2.Resource, *resource.SyncOpResults, error) {
	var rv []*v2.Resource

	roles, resp, err := r.client.GetRoles(ctx)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, nil, err
	}
	defer resp.Body.Close()

	annos, err := parseResp(resp)
	if err != nil {
		return nil, nil, err
	}

	for _, role := range roles {
		roleCopy := role
		rr, err := roleResource(roleCopy, parentId)
		if err != nil {
			return nil, nil, err
		}
		rv = append(rv, rr)
	}

	return rv, &resource.SyncOpResults{Annotations: annos}, nil
}

func (r *roleResourceType) Entitlements(_ context.Context, res *v2.Resource, _ resource.SyncOpAttrs) ([]*v2.Entitlement, *resource.SyncOpResults, error) {
	var rv []*v2.Entitlement

	roleOptions := []ent.EntitlementOption{
		ent.WithGrantableTo(resourceTypeUser),
		ent.WithDescription(fmt.Sprintf("Role %s in zoom", res.DisplayName)),
		ent.WithDisplayName(fmt.Sprintf("%s role %s", res.DisplayName, memberEntitlement)),
	}

	en := ent.NewPermissionEntitlement(res, memberEntitlement, roleOptions...)
	rv = append(rv, en)

	return rv, &resource.SyncOpResults{}, nil
}

func (r *roleResourceType) Grants(ctx context.Context, res *v2.Resource, opts resource.SyncOpAttrs) ([]*v2.Grant, *resource.SyncOpResults, error) {
	var rv []*v2.Grant
	var pageToken string

	bag, page, err := parsePageToken(opts.PageToken.Token, &v2.ResourceId{ResourceType: resourceTypeRole.Id})
	if err != nil {
		return nil, nil, err
	}

	roleMembers, nextToken, resp, err := r.client.GetRoleMembers(ctx, res.Id.Resource, page)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, nil, err
	}
	defer resp.Body.Close()

	if nextToken != "" {
		pageToken, err = bag.NextToken(nextToken)
		if err != nil {
			return nil, nil, err
		}
	}

	annos, err := parseResp(resp)
	if err != nil {
		return nil, nil, err
	}

	for _, member := range roleMembers {
		memberCopy := member
		ur, err := userResource(memberCopy, res.Id)
		if err != nil {
			return nil, nil, err
		}

		grant := grant.NewGrant(res, memberEntitlement, ur.Id)
		rv = append(rv, grant)
	}

	return rv, &resource.SyncOpResults{NextPageToken: pageToken, Annotations: annos}, nil
}

func (r *roleResourceType) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) ([]*v2.Grant, annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	if principal.Id.ResourceType != resourceTypeUser.Id {
		l.Debug(
			"baton-zoom: only users can be granted role membership",
			zap.String("principal_type", principal.Id.ResourceType),
			zap.String("principal_id", principal.Id.Resource),
		)
		return nil, nil, fmt.Errorf("baton-zoom: only users can be granted role membership")
	}

	result := []*v2.Grant{
		grant.NewGrant(entitlement.GetResource(), entitlement.GetSlug(), principal.GetId()),
	}

	user, resp, err := r.client.GetUser(ctx, principal.Id.Resource)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, nil, fmt.Errorf("baton-zoom: failed to get user before granting role: %w", err)
	}
	resp.Body.Close()

	if user.Status == userStatusInactive {
		return nil, nil, fmt.Errorf("baton-zoom: cannot grant role to inactive user %s", principal.Id.Resource)
	}

	if user.RoleID == entitlement.Resource.Id.Resource {
		return result, annotations.New(&v2.GrantAlreadyExists{}), nil
	}

	err = r.client.AssignRole(ctx, entitlement.Resource.Id.Resource, principal.Id.Resource)
	if err != nil {
		return nil, nil, fmt.Errorf("baton-zoom: failed to assign role to user: %w", err)
	}

	return result, nil, nil
}

func (r *roleResourceType) Revoke(ctx context.Context, grant *v2.Grant) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	entitlement := grant.Entitlement
	principal := grant.Principal

	if principal.Id.ResourceType != resourceTypeUser.Id {
		l.Debug(
			"baton-zoom: only users can have role membership revoked",
			zap.String("principal_type", principal.Id.ResourceType),
			zap.String("principal_id", principal.Id.Resource),
		)
		return nil, fmt.Errorf("baton-zoom: only users can have role membership revoked")
	}

	user, resp, err := r.client.GetUser(ctx, principal.Id.Resource)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, fmt.Errorf("baton-zoom: failed to get user before revoking role: %w", err)
	}
	resp.Body.Close()

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
