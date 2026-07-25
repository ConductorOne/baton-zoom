package connector

import (
	"context"
	"fmt"
	"strconv"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	"github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-zoom/pkg/zoom"
	"google.golang.org/protobuf/proto"
)

const (
	// userTypeProfileKey carries the user's Zoom license tier (User.type) on
	// the resource profile so userBuilder.Grants can emit the principal-side
	// license grant without an extra GET /v2/users/{id} call.
	userTypeProfileKey = "type"
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
	profile := map[string]any{
		firstNameKey:       user.FirstName,
		lastNameKey:        user.LastName,
		"login":            user.Email,
		"user_id":          user.ID,
		userTypeProfileKey: int64(user.Type),
	}

	var userStatus v2.Status_ResourceStatus

	switch user.Status {
	case userStatusInactive:
		userStatus = v2.Status_RESOURCE_STATUS_DISABLED
	case userStatusActive:
		userStatus = v2.Status_RESOURCE_STATUS_ENABLED
	default:
		userStatus = v2.Status_RESOURCE_STATUS_UNSPECIFIED
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
		resource.WithResourceStatus(userStatus, ""),
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
		b.Push(pagination.PageState{ResourceTypeID: resourceTypeUser.Id, ResourceID: userStatusActive})
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

// Grants emits the principal-side license grant for a single user resource.
// License is a derived resource type (no /licenses endpoint in Zoom), so its
// grants are produced from the user side using the User.type value stashed in
// the resource profile during List(). Values outside the modeled tiers
// yield no grant — the matching License resource was never listed, so
// emitting one would dangle.
func (u *userResourceType) Grants(_ context.Context, res *v2.Resource, _ resource.SyncOpAttrs) ([]*v2.Grant, *resource.SyncOpResults, error) {
	profile := resource.GetProfile(res).AsMap()
	rawType, ok := profile[userTypeProfileKey]
	if !ok {
		return nil, &resource.SyncOpResults{}, nil
	}

	// structpb decodes JSON numbers as float64; the int / int64 branches
	// cover the unlikely path where the profile is constructed in-process
	// without going through a JSON round-trip.
	var userType int
	switch v := rawType.(type) {
	case float64:
		userType = int(v)
	case int:
		userType = v
	case int64:
		userType = int(v)
	default:
		return nil, &resource.SyncOpResults{}, nil
	}

	if !isLicenseTier(zoom.UserType(userType)) {
		return nil, &resource.SyncOpResults{}, nil
	}

	licenseResource := &v2.Resource{
		Id: &v2.ResourceId{
			ResourceType: resourceTypeLicense.Id,
			Resource:     strconv.Itoa(userType),
		},
	}

	return []*v2.Grant{
		grant.NewGrant(licenseResource, assignedEntitlement, res.Id),
	}, &resource.SyncOpResults{}, nil
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

// userBuilder returns the user syncer. Users have no entitlements of their
// own, so the user resource type always skips the entitlements pass. The only
// grants users emit are license tiers, so when skipLicenseGrants is true (the
// license resource type is excluded from the sync) the grants pass is skipped
// too — the license resources those grants target wouldn't exist in the sync.
func userBuilder(client *zoom.Client, syncInactiveUsers bool, skipLicenseGrants bool) *userResourceType {
	resourceType := proto.Clone(resourceTypeUser).(*v2.ResourceType)
	userAnnos := annotations.Annotations(resourceType.GetAnnotations())
	if skipLicenseGrants {
		userAnnos.Update(&v2.SkipEntitlementsAndGrants{})
	} else {
		userAnnos.Update(&v2.SkipEntitlements{})
	}
	resourceType.Annotations = userAnnos

	return &userResourceType{
		resourceType:      resourceType,
		client:            client,
		syncInactiveUsers: syncInactiveUsers,
	}
}
