package connector

import (
	"context"

	"github.com/ConductorOne/baton-zoom/pkg/zoom"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	resource "github.com/conductorone/baton-sdk/pkg/types/resource"
)

type userResourceType struct {
	resourceType *v2.ResourceType
	client       *zoom.Client
}

func (u *userResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return u.resourceType
}

// Create a new connector resource for a Zoom user.
func userResource(user zoom.User, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	profile := map[string]interface{}{
		"first_name": user.FirstName,
		"last_name":  user.LastName,
		"login":      user.Email,
		"user_id":    user.ID,
	}

	var userStatus v2.UserTrait_Status_Status

	switch user.Status {
	case "pending":
		userStatus = v2.UserTrait_Status_STATUS_UNSPECIFIED
	case "inactive":
		userStatus = v2.UserTrait_Status_STATUS_DISABLED
	case "active":
		userStatus = v2.UserTrait_Status_STATUS_ENABLED
	}

	userTraitTraitOptions := []resource.UserTraitOption{
		resource.WithUserProfile(profile),
		resource.WithStatus(userStatus),
		resource.WithEmail(user.Email, true),
	}

	ret, err := resource.NewUserResource(
		user.DisplayName,
		resourceTypeUser,
		user.ID,
		userTraitTraitOptions,
		resource.WithParentResourceID(parentResourceID),
	)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func (u *userResourceType) List(ctx context.Context, parentId *v2.ResourceId, token *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	var pageToken string
	var rv []*v2.Resource

	bag, page, err := parsePageToken(token.Token, &v2.ResourceId{ResourceType: resourceTypeGroup.Id})
	if err != nil {
		return nil, "", nil, err
	}

	users, nextPage, resp, err := u.client.GetUsers(ctx, page)
	if err != nil {
		return nil, "", nil, err
	}
	resp.Body.Close()

	if nextPage != "" {
		pageToken, err = bag.NextToken(nextPage)
		if err != nil {
			return nil, "", nil, err
		}
	}

	annos, err := parseResp(resp)
	if err != nil {
		return nil, "", nil, err
	}

	for _, user := range users {
		userCopy := user
		ur, err := userResource(userCopy, parentId)
		if err != nil {
			return nil, "", nil, err
		}
		rv = append(rv, ur)
	}

	return rv, pageToken, annos, nil
}

func (u *userResourceType) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func (u *userResourceType) Grants(ctx context.Context, resource *v2.Resource, token *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func userBuilder(client *zoom.Client) *userResourceType {
	return &userResourceType{
		resourceType: resourceTypeUser,
		client:       client,
	}
}
