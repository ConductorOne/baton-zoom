package connector

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	"github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/conductorone/baton-zoom/pkg/zoom"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"google.golang.org/grpc/codes"
)

type userResourceType struct {
	resourceType      *v2.ResourceType
	client            *zoom.Client
	syncInactiveUsers bool
	syncResourceTypes map[string]struct{}
}

func (u *userResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return u.resourceType
}

// Create a new connector resource for a Zoom user.
func userResource(user *zoom.User, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	profile := map[string]any{
		firstNameKey:       user.FirstName,
		lastNameKey:        user.LastName,
		loginKey:           user.Email,
		userIDKey:          user.ID,
		userTypeProfileKey: int64(user.Type),
	}

	userTraitTraitOptions := []resource.UserTraitOption{
		resource.WithEmail(user.Email, true),
	}

	ret, err := resource.NewUserResource(
		user.DisplayName,
		resourceTypeUser,
		user.ID,
		userTraitTraitOptions,
		resource.WithParentResourceID(parentResourceID),
		resource.WithResourceProfile(profile),
		resource.WithResourceStatus(userTraitStatus(user.Status), ""),
	)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func (u *userResourceType) List(ctx context.Context, parentId *v2.ResourceId, opts resource.SyncOpAttrs) ([]*v2.Resource, *resource.SyncOpResults, error) {
	b := &pagination.Bag{}
	err := b.Unmarshal(opts.PageToken.Token)
	if err != nil {
		return nil, nil, uhttp.WrapErrors(
			codes.InvalidArgument,
			"baton-zoom: list users: invalid page token",
			err,
		)
	}

	// Initialize: push statuses in reverse order so active is processed first.
	// Inactive users are only included when the flag is enabled.
	// Pending users are omitted here and synced as the Invite resource type.
	if b.Current() == nil {
		if u.syncInactiveUsers {
			b.Push(pagination.PageState{ResourceTypeID: resourceTypeUser.Id, ResourceID: userStatusInactive})
		}
		b.Push(pagination.PageState{ResourceTypeID: resourceTypeUser.Id, ResourceID: userStatusActive})
	}

	users, nextPage, annos, err := u.client.GetUsers(ctx, b.PageToken(), b.Current().ResourceID)
	if err != nil {
		return nil, &resource.SyncOpResults{Annotations: annos}, fmt.Errorf("baton-zoom: list users: %w", err)
	}

	// Advance the bag: if no next page, pop the current status state; otherwise update its token.
	err = b.Next(nextPage)
	if err != nil {
		return nil, nil, err
	}
	pageToken, err := b.Marshal()
	if err != nil {
		return nil, nil, err
	}

	rv := make([]*v2.Resource, 0, len(users))
	for _, user := range users {
		ur, err := userResource(user, parentId)
		if err != nil {
			return nil, nil, err
		}
		rv = append(rv, ur)
	}

	return rv, &resource.SyncOpResults{NextPageToken: pageToken, Annotations: annos}, nil
}

func (u *userResourceType) Entitlements(_ context.Context, _ *v2.Resource, _ resource.SyncOpAttrs) ([]*v2.Entitlement, *resource.SyncOpResults, error) {
	return nil, nil, nil
}

// Grants emits the user's group, role and license memberships from the principal
// side. GET /v2/users/{userId} returns group_ids, role_id and type together, so
// the group and role builders never have to rescan every member to invert the
// relationship.
func (u *userResourceType) Grants(ctx context.Context, res *v2.Resource, _ resource.SyncOpAttrs) ([]*v2.Grant, *resource.SyncOpResults, error) {
	syncGroups := willSyncResourceType(u.syncResourceTypes, resourceTypeGroup.Id)
	syncRoles := willSyncResourceType(u.syncResourceTypes, resourceTypeRole.Id)
	syncLicenses := willSyncResourceType(u.syncResourceTypes, resourceTypeLicense.Id)
	if !syncGroups && !syncRoles && !syncLicenses {
		return nil, nil, nil
	}

	user, annos, err := u.client.GetUser(ctx, res.Id.Resource)
	if err != nil {
		return nil, &resource.SyncOpResults{Annotations: annos}, fmt.Errorf("baton-zoom: list user grants: %w", err)
	}

	var grants []*v2.Grant

	if syncGroups {
		for _, groupID := range user.GroupIDs {
			if groupID == "" {
				continue
			}
			grants = append(grants, grant.NewGrant(
				grantResource(resourceTypeGroup.Id, groupID),
				memberEntitlement,
				res.Id,
			))
		}
	}

	if syncRoles && user.RoleID != "" {
		grants = append(grants, grant.NewGrant(
			grantResource(resourceTypeRole.Id, user.RoleID),
			memberEntitlement,
			res.Id,
		))
	}

	if syncLicenses && isLicenseTier(zoom.UserType(user.Type)) {
		grants = append(grants, grant.NewGrant(
			grantResource(resourceTypeLicense.Id, strconv.Itoa(user.Type)),
			assignedEntitlement,
			res.Id,
		))
	}

	return grants, &resource.SyncOpResults{Annotations: annos}, nil
}

func grantResource(resourceTypeID, resourceID string) *v2.Resource {
	return &v2.Resource{
		Id: &v2.ResourceId{
			ResourceType: resourceTypeID,
			Resource:     resourceID,
		},
	}
}

// isLicenseTier reports whether the given Zoom user type maps to a License
// resource we sync. Basic / Licensed / Unassigned are the modeled tiers;
// any other value is treated as "no license".
func isLicenseTier(t zoom.UserType) bool {
	switch t {
	case zoom.BasicUser, zoom.LicensedUser, zoom.UnassignedUser:
		return true
	default:
		return false
	}
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
		// The conflict already proves the account exists. Report it as such and
		// let the next user sync correlate the resource.
		if zoom.IsAPIError(err, http.StatusConflict, zoom.UserAlreadyExistsErrorCode) {
			ctxzap.Extract(ctx).Debug("baton-zoom: account already exists in Zoom")
			return &v2.CreateAccountResponse_AlreadyExistsResult{IsCreateAccountResult: true}, nil, nil, nil
		}
		return nil, nil, nil, fmt.Errorf("baton-zoom: create account: %w", err)
	}

	userResource, err := userResource(&zoom.User{
		ID:          newUser.Id,
		FirstName:   newUser.FirstName,
		LastName:    newUser.LastName,
		Email:       newUser.Email,
		Type:        newUser.Type,
		DisplayName: newUserInfo.UserInfo.DisplayName,
		Status:      userStatusPending,
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

	email, err := requiredStringProfileField(pMap, emailKey, "email")
	if err != nil {
		return nil, err
	}

	firstName, err := requiredStringProfileField(pMap, firstNameKey, "first name")
	if err != nil {
		return nil, err
	}

	lastName, err := requiredStringProfileField(pMap, lastNameKey, "last name")
	if err != nil {
		return nil, err
	}

	displayName, err := requiredStringProfileField(pMap, displayNameKey, "display name")
	if err != nil {
		return nil, err
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

func requiredStringProfileField(profile map[string]any, key, displayName string) (string, error) {
	rawValue, ok := profile[key]
	if !ok || rawValue == nil {
		return "", uhttp.WrapErrors(
			codes.InvalidArgument,
			fmt.Sprintf("baton-zoom: create account: %s is required", displayName),
		)
	}

	value, ok := rawValue.(string)
	if !ok {
		return "", uhttp.WrapErrors(
			codes.InvalidArgument,
			fmt.Sprintf("baton-zoom: create account: invalid %s format: expected a string", displayName),
		)
	}
	if value == "" {
		return "", uhttp.WrapErrors(
			codes.InvalidArgument,
			fmt.Sprintf("baton-zoom: create account: %s is required", displayName),
		)
	}

	return value, nil
}

func (u *userResourceType) Delete(ctx context.Context, principal *v2.ResourceId) (annotations.Annotations, error) {
	userID := principal.Resource

	err := u.client.DeleteUser(ctx, userID, zoom.DeleteUserOptions{Action: zoom.Delete})
	if err != nil {
		if zoom.IsAPIError(err, http.StatusNotFound, zoom.UserNotFoundErrorCode) {
			return nil, nil
		}
		return nil, fmt.Errorf("baton-zoom: failed to delete user %s: %w", userID, err)
	}

	return nil, nil
}

func userBuilder(client *zoom.Client, syncInactiveUsers bool, syncResourceTypes map[string]struct{}) *userResourceType {
	return &userResourceType{
		resourceType:      resourceTypeUser,
		client:            client,
		syncInactiveUsers: syncInactiveUsers,
		syncResourceTypes: syncResourceTypes,
	}
}
