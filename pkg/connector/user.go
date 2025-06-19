package connector

import (
	"context"
	"fmt"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-zoom/pkg/zoom"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	default:
		userStatus = v2.UserTrait_Status_STATUS_UNSPECIFIED
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

func (u *userResourceType) Entitlements(_ context.Context, _ *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func (u *userResourceType) Grants(_ context.Context, _ *v2.Resource, _ *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func (u *userResourceType) CreateAccountCapabilityDetails(_ context.Context) (*v2.CredentialDetailsAccountProvisioning, annotations.Annotations, error) {
	return &v2.CredentialDetailsAccountProvisioning{
		SupportedCredentialOptions: []v2.CapabilityDetailCredentialOption{
			v2.CapabilityDetailCredentialOption_CAPABILITY_DETAIL_CREDENTIAL_OPTION_NO_PASSWORD,
		},
		PreferredCredentialOption: v2.CapabilityDetailCredentialOption_CAPABILITY_DETAIL_CREDENTIAL_OPTION_NO_PASSWORD,
	}, nil, nil
}

func (u *userResourceType) CreateAccount(
	ctx context.Context,
	accountInfo *v2.AccountInfo,
	_ *v2.CredentialOptions,
) (connectorbuilder.CreateAccountResponse, []*v2.PlaintextData, annotations.Annotations, error) {
	newUserInfo, err := createNewUserInfo(accountInfo)
	if err != nil {
		return nil, nil, nil, err
	}

	newUser, err := u.client.CreateUser(ctx, newUserInfo)
	if err != nil {
		return nil, nil, nil, err
	}

	userResource, err := userResource(zoom.User{
		ID:        newUser.Id,
		FirstName: newUser.FirstName,
		LastName:  newUser.LastName,
		Email:     newUser.Email,
		Type:      newUser.Type,
	}, nil)
	if err != nil {
		return nil, nil, nil, err
	}

	caResponse := &v2.CreateAccountResponse_SuccessResult{
		Resource: userResource,
	}

	return caResponse, nil, nil, nil
}

func createNewUserInfo(accountInfo *v2.AccountInfo) (*zoom.UserCreationBody, error) {
	pMap := accountInfo.Profile.AsMap()

	email, ok := pMap["email"].(string)
	if !ok || email == "" {
		return nil, fmt.Errorf("email is required")
	}

	firstName, ok := pMap["first_name"].(string)
	if !ok || firstName == "" {
		return nil, fmt.Errorf("first name is required")
	}

	lastName, ok := pMap["last_name"].(string)
	if !ok || lastName == "" {
		return nil, fmt.Errorf("last name is required")
	}

	displayName, ok := pMap["display_name"].(string)
	if !ok || displayName == "" {
		return nil, fmt.Errorf("display name is required")
	}

	newUserInfo := &zoom.UserCreationBody{
		Action: zoom.CreateUser,
		UserInfo: zoom.UserCreationInfo{
			Type:        zoom.BasicUser,
			FirstName:   firstName,
			LastName:    lastName,
			Email:       email,
			DisplayName: displayName,
		},
	}

	return newUserInfo, nil
}

func (u *userResourceType) Delete(ctx context.Context, principal *v2.ResourceId) (annotations.Annotations, error) {
	userID := principal.Resource

	err := u.client.DeleteUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	_, resp, err := u.client.GetUser(ctx, userID)
	if err == nil || status.Code(err) != codes.NotFound {
		return nil, fmt.Errorf("error deleting user. User %s still exists", userID)
	}
	defer resp.Body.Close()

	return nil, nil
}

func userBuilder(client *zoom.Client) *userResourceType {
	return &userResourceType{
		resourceType: resourceTypeUser,
		client:       client,
	}
}
