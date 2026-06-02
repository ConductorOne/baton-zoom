package connector

import (
	"context"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-zoom/pkg/zoom"
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
func inviteResource(user zoom.User, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	profile := map[string]any{
		firstNameKey: user.FirstName,
		lastNameKey:  user.LastName,
		"login":      user.Email,
	}

	userTraitOptions := []resource.UserTraitOption{
		resource.WithUserProfile(profile),
		resource.WithStatus(v2.UserTrait_Status_STATUS_UNSPECIFIED),
		resource.WithEmail(user.Email, true),
	}

	ret, err := resource.NewUserResource(
		user.DisplayName,
		resourceTypeInvite,
		user.Email,
		userTraitOptions,
		resource.WithParentResourceID(parentResourceID),
	)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func (i *inviteResourceType) List(ctx context.Context, parentId *v2.ResourceId, opts resource.SyncOpAttrs) ([]*v2.Resource, *resource.SyncOpResults, error) {
	var rv []*v2.Resource

	users, nextPage, resp, err := i.client.GetUsers(ctx, opts.PageToken.Token, "pending")
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

	for _, user := range users {
		userCopy := user
		ur, err := inviteResource(userCopy, parentId)
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

func inviteBuilder(client *zoom.Client) *inviteResourceType {
	return &inviteResourceType{
		resourceType: resourceTypeInvite,
		client:       client,
	}
}
