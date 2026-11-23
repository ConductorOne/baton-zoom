package connector

import (
	"context"
	"fmt"
	"net/http"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/conductorone/baton-zoom/pkg/zoom"
	"google.golang.org/grpc/codes"
)

type inviteResourceType struct {
	resourceType *v2.ResourceType
	client       *zoom.Client
}

func (i *inviteResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return i.resourceType
}

// inviteResource creates a connector resource for a pending Zoom user (invite).
// Pending users have no Zoom ID yet, so the email is used as the stable resource identifier.
func inviteResource(user *zoom.User, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	profile := map[string]any{
		firstNameKey: user.FirstName,
		lastNameKey:  user.LastName,
		loginKey:     user.Email,
	}

	userTraitOptions := []resource.UserTraitOption{
		resource.WithEmail(user.Email, true),
	}

	ret, err := resource.NewUserResource(
		user.DisplayName,
		resourceTypeInvite,
		user.Email,
		userTraitOptions,
		resource.WithParentResourceID(parentResourceID),
		resource.WithResourceProfile(profile),
		resource.WithResourceStatus(v2.Status_RESOURCE_STATUS_PENDING, ""),
	)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func (i *inviteResourceType) List(ctx context.Context, parentId *v2.ResourceId, opts resource.SyncOpAttrs) ([]*v2.Resource, *resource.SyncOpResults, error) {
	users, nextPage, annos, err := i.client.GetUsers(ctx, opts.PageToken.Token, userStatusPending)
	if err != nil {
		return nil, &resource.SyncOpResults{Annotations: annos}, fmt.Errorf("baton-zoom: list pending invitations: %w", err)
	}

	rv := make([]*v2.Resource, 0, len(users))
	for _, user := range users {
		ur, err := inviteResource(user, parentId)
		if err != nil {
			return nil, nil, err
		}
		rv = append(rv, ur)
	}

	return rv, &resource.SyncOpResults{NextPageToken: nextPage, Annotations: annos}, nil
}

func (i *inviteResourceType) Entitlements(_ context.Context, _ *v2.Resource, _ resource.SyncOpAttrs) ([]*v2.Entitlement, *resource.SyncOpResults, error) {
	return nil, &resource.SyncOpResults{}, nil
}

func (i *inviteResourceType) Grants(_ context.Context, _ *v2.Resource, _ resource.SyncOpAttrs) ([]*v2.Grant, *resource.SyncOpResults, error) {
	return nil, &resource.SyncOpResults{}, nil
}

func (i *inviteResourceType) Delete(ctx context.Context, principal *v2.ResourceId) (annotations.Annotations, error) {
	if principal.GetResourceType() != resourceTypeInvite.Id || principal.GetResource() == "" {
		return nil, uhttp.WrapErrors(codes.InvalidArgument, "baton-zoom: delete invite: a non-empty invite resource ID is required")
	}
	email := principal.GetResource()
	user, annos, err := i.client.GetUser(ctx, email)
	if err != nil {
		if zoom.IsAPIError(err, http.StatusNotFound, zoom.UserNotFoundErrorCode) {
			return annos, nil
		}
		return annos, fmt.Errorf("baton-zoom: get pending invite: %w", err)
	}
	if user.Status != userStatusPending {
		return annos, uhttp.WrapErrors(codes.FailedPrecondition, "baton-zoom: delete invite: user is no longer pending; refusing to disassociate")
	}

	// Zoom has no conditional delete: this best-effort preflight cannot prevent
	// disassociation if the user accepts the invitation between GET and DELETE.
	err = i.client.DeleteUser(ctx, email, zoom.DeleteUserOptions{Action: zoom.Disassociate})
	if err != nil {
		if zoom.IsAPIError(err, http.StatusNotFound, zoom.UserNotFoundErrorCode) {
			return annos, nil
		}
		return annos, fmt.Errorf("baton-zoom: delete pending invite: %w", err)
	}
	return annos, nil
}

func inviteBuilder(client *zoom.Client) *inviteResourceType {
	return &inviteResourceType{
		resourceType: resourceTypeInvite,
		client:       client,
	}
}
