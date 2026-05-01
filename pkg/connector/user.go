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
)

type userResourceType struct {
	resourceType      *v2.ResourceType
	client            *zoom.Client
	syncInactiveUsers bool
}

func (u *userResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return u.resourceType
}

// Create a new connector resource for a Zoom user.
func userResource(user zoom.User, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	profile := map[string]interface{}{
		firstNameKey: user.FirstName,
		lastNameKey:  user.LastName,
		"login":      user.Email,
		"user_id":    user.ID,
	}

	var userStatus v2.UserTrait_Status_Status

	switch user.Status {
	case userStatusInactive:
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

func (u *userResourceType) List(ctx context.Context, parentId *v2.ResourceId, opts resource.SyncOpAttrs) ([]*v2.Resource, *resource.SyncOpResults, error) {
	var rv []*v2.Resource

	b := &pagination.Bag{}
	err := b.Unmarshal(opts.PageToken.Token)
	if err != nil {
		return nil, nil, err
	}

	// Initialize: push statuses in reverse order so active is processed first.
	// Inactive users are only included when the flag is enabled.
	// Pending users are omitted — they have no ID yet and are synced via the Invite resource type.
	if b.Current() == nil {
		if u.syncInactiveUsers {
			b.Push(pagination.PageState{ResourceTypeID: resourceTypeUser.Id, ResourceID: userStatusInactive})
		}
		b.Push(pagination.PageState{ResourceTypeID: resourceTypeUser.Id, ResourceID: "active"})
	}

	status := b.Current().ResourceID
	page := b.PageToken()

	users, nextPage, resp, err := u.client.GetUsers(ctx, page, status)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, nil, err
	}
	defer resp.Body.Close()

	// Advance the bag: if no next page, pops the current status state; otherwise updates its token.
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
		userCopy := user
		ur, err := userResource(userCopy, parentId)
		if err != nil {
			return nil, nil, err
		}
		rv = append(rv, ur)
	}

	return rv, &resource.SyncOpResults{NextPageToken: pageToken, Annotations: annos}, nil
}

func (u *userResourceType) Entitlements(_ context.Context, _ *v2.Resource, _ resource.SyncOpAttrs) ([]*v2.Entitlement, *resource.SyncOpResults, error) {
	return nil, &resource.SyncOpResults{}, nil
}

func (u *userResourceType) Grants(_ context.Context, _ *v2.Resource, _ resource.SyncOpAttrs) ([]*v2.Grant, *resource.SyncOpResults, error) {
	return nil, &resource.SyncOpResults{}, nil
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
	_ *v2.LocalCredentialOptions,
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
		return nil, fmt.Errorf("baton-zoom: failed to delete user %s: %w", userID, err)
	}

	return nil, nil
}

func userBuilder(client *zoom.Client, syncInactiveUsers bool) *userResourceType {
	return &userResourceType{
		resourceType:      resourceTypeUser,
		client:            client,
		syncInactiveUsers: syncInactiveUsers,
	}
}
