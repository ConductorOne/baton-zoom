package connector

import (
	"context"
	"fmt"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	grant "github.com/conductorone/baton-sdk/pkg/types/grant"
	resource "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-zoom/pkg/zoom"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

const assignedEntitlement = "assigned"

type licenseInfo struct {
	ID          string
	DisplayName string
	UserType    int
}

var licenseTypes = []licenseInfo{
	{ID: "basic", DisplayName: "Basic", UserType: int(zoom.BasicUser)},
	{ID: "licensed", DisplayName: "Licensed", UserType: int(zoom.LicensedUser)},
	{ID: "none", DisplayName: "None", UserType: int(zoom.NoneUser)},
}

func licenseTypeForResource(resourceID string) (int, bool) {
	for _, lt := range licenseTypes {
		if lt.ID == resourceID {
			return lt.UserType, true
		}
	}
	return 0, false
}

type licenseResourceType struct {
	resourceType      *v2.ResourceType
	client            *zoom.Client
	syncInactiveUsers bool
}

func (l *licenseResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return l.resourceType
}

func licenseResource(license licenseInfo, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	ret, err := resource.NewResource(
		license.DisplayName,
		resourceTypeLicense,
		license.ID,
		resource.WithParentResourceID(parentResourceID),
	)
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func (l *licenseResourceType) List(_ context.Context, parentId *v2.ResourceId, _ resource.SyncOpAttrs) ([]*v2.Resource, *resource.SyncOpResults, error) {
	var rv []*v2.Resource
	for _, lt := range licenseTypes {
		r, err := licenseResource(lt, parentId)
		if err != nil {
			return nil, nil, err
		}
		rv = append(rv, r)
	}
	return rv, &resource.SyncOpResults{}, nil
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
	targetType, ok := licenseTypeForResource(r.Id.Resource)
	if !ok {
		return nil, nil, fmt.Errorf("baton-zoom: unknown license resource %s", r.Id.Resource)
	}

	var rv []*v2.Grant

	b := &pagination.Bag{}
	err := b.Unmarshal(opts.PageToken.Token)
	if err != nil {
		return nil, nil, err
	}

	if b.Current() == nil {
		if l.syncInactiveUsers {
			b.Push(pagination.PageState{ResourceTypeID: resourceTypeLicense.Id, ResourceID: userStatusInactive})
		}
		b.Push(pagination.PageState{ResourceTypeID: resourceTypeLicense.Id, ResourceID: "active"})
	}

	status := b.Current().ResourceID
	page := b.PageToken()

	users, nextPage, resp, err := l.client.GetUsers(ctx, page, status)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, nil, err
	}
	defer resp.Body.Close()

	err = b.Next(nextPage)
	if err != nil {
		return nil, nil, err
	}

	pageToken, err := b.Marshal()
	if err != nil {
		return nil, nil, err
	}

	annos, err := parseResp(resp)
	if err != nil {
		return nil, nil, err
	}

	for _, user := range users {
		if user.Type == targetType {
			userCopy := user
			ur, err := userResource(userCopy, r.Id)
			if err != nil {
				return nil, nil, err
			}
			rv = append(rv, grant.NewGrant(r, assignedEntitlement, ur.Id))
		}
	}

	return rv, &resource.SyncOpResults{NextPageToken: pageToken, Annotations: annos}, nil
}

func (l *licenseResourceType) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) ([]*v2.Grant, annotations.Annotations, error) {
	ll := ctxzap.Extract(ctx)

	if principal.Id.ResourceType != resourceTypeUser.Id {
		ll.Warn(
			"baton-zoom: only users can be granted a license",
			zap.String("principal_type", principal.Id.ResourceType),
			zap.String("principal_id", principal.Id.Resource),
		)
		return nil, nil, fmt.Errorf("baton-zoom: only users can be granted a license")
	}

	targetType, ok := licenseTypeForResource(entitlement.Resource.Id.Resource)
	if !ok {
		return nil, nil, fmt.Errorf("baton-zoom: unknown license resource %s", entitlement.Resource.Id.Resource)
	}

	user, resp, err := l.client.GetUser(ctx, principal.Id.Resource)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, nil, fmt.Errorf("baton-zoom: failed to get user before granting license: %w", err)
	}
	resp.Body.Close()

	if user.Type == targetType {
		return nil, annotations.New(&v2.GrantAlreadyExists{}), nil
	}

	err = l.client.UpdateUserType(ctx, principal.Id.Resource, targetType)
	if err != nil {
		return nil, nil, fmt.Errorf("baton-zoom: failed to update user license type: %w", err)
	}

	return nil, nil, nil
}

func (l *licenseResourceType) Revoke(ctx context.Context, g *v2.Grant) (annotations.Annotations, error) {
	ll := ctxzap.Extract(ctx)

	entitlement := g.Entitlement
	principal := g.Principal

	if principal.Id.ResourceType != resourceTypeUser.Id {
		ll.Warn(
			"baton-zoom: only users can have a license revoked",
			zap.String("principal_type", principal.Id.ResourceType),
			zap.String("principal_id", principal.Id.Resource),
		)
		return nil, fmt.Errorf("baton-zoom: only users can have a license revoked")
	}

	targetType, ok := licenseTypeForResource(entitlement.Resource.Id.Resource)
	if !ok {
		return nil, fmt.Errorf("baton-zoom: unknown license resource %s", entitlement.Resource.Id.Resource)
	}

	user, resp, err := l.client.GetUser(ctx, principal.Id.Resource)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, fmt.Errorf("baton-zoom: failed to get user before revoking license: %w", err)
	}
	resp.Body.Close()

	if user.Type != targetType {
		return annotations.New(&v2.GrantAlreadyRevoked{}), nil
	}

	err = l.client.UpdateUserType(ctx, principal.Id.Resource, int(zoom.BasicUser))
	if err != nil {
		return nil, fmt.Errorf("baton-zoom: failed to downgrade user license type: %w", err)
	}

	return nil, nil
}

func licenseBuilder(client *zoom.Client, syncInactiveUsers bool) *licenseResourceType {
	return &licenseResourceType{
		resourceType:      resourceTypeLicense,
		client:            client,
		syncInactiveUsers: syncInactiveUsers,
	}
}
