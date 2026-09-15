package connector

import (
	"testing"

	"github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-zoom/pkg/zoom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserGrantsUsesListProfileWithoutRefetchingUser(t *testing.T) {
	t.Parallel()

	user, err := userResource(&zoom.User{
		ID:       "user-id",
		Type:     int(zoom.LicensedUser),
		RoleID:   "role-id",
		GroupIDs: []string{"group-a", "", "group-b"},
	}, nil)
	require.NoError(t, err)

	tests := []struct {
		name              string
		syncResourceTypes map[string]struct{}
		wantByType        map[string]int
	}{
		{
			name: "default selection emits all grants",
			wantByType: map[string]int{
				resourceTypeGroup.Id:   2,
				resourceTypeRole.Id:    1,
				resourceTypeLicense.Id: 1,
			},
		},
		{
			name: "group selection emits group grants",
			syncResourceTypes: map[string]struct{}{
				resourceTypeGroup.Id: {},
			},
			wantByType: map[string]int{resourceTypeGroup.Id: 2},
		},
		{
			name: "role selection emits role grant",
			syncResourceTypes: map[string]struct{}{
				resourceTypeRole.Id: {},
			},
			wantByType: map[string]int{resourceTypeRole.Id: 1},
		},
		{
			name: "license selection emits license grant",
			syncResourceTypes: map[string]struct{}{
				resourceTypeLicense.Id: {},
			},
			wantByType: map[string]int{resourceTypeLicense.Id: 1},
		},
		{
			name: "unrelated selection emits no grants",
			syncResourceTypes: map[string]struct{}{
				resourceTypeUser.Id: {},
			},
			wantByType: map[string]int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := &userResourceType{
				client:            nil,
				syncResourceTypes: tt.syncResourceTypes,
			}
			grants, results, err := builder.Grants(t.Context(), user, resource.SyncOpAttrs{})
			require.NoError(t, err)
			assert.Nil(t, results)

			gotByType := make(map[string]int)
			for _, g := range grants {
				gotByType[g.GetEntitlement().GetResource().GetId().GetResourceType()]++
			}
			assert.Equal(t, tt.wantByType, gotByType)
		})
	}
}

func TestUserResourcePersistsGrantInputs(t *testing.T) {
	t.Parallel()

	user, err := userResource(&zoom.User{
		ID:       "user-id",
		Type:     int(zoom.LicensedUser),
		RoleID:   "role-id",
		GroupIDs: []string{"group-a", "group-b"},
	}, nil)
	require.NoError(t, err)

	profile := resource.GetProfile(user)
	userType, ok := resource.GetProfileInt64Value(profile, userTypeProfileKey)
	require.True(t, ok)
	assert.Equal(t, int64(zoom.LicensedUser), userType)

	roleID, ok := resource.GetProfileStringValue(profile, userRoleProfileKey)
	require.True(t, ok)
	assert.Equal(t, "role-id", roleID)

	var groupIDs []string
	for _, value := range profile.GetFields()[userGroupIDsKey].GetListValue().GetValues() {
		groupIDs = append(groupIDs, value.GetStringValue())
	}
	assert.Equal(t, []string{"group-a", "group-b"}, groupIDs)
}

func TestUserGrantsOmitsUnknownLicenseType(t *testing.T) {
	t.Parallel()

	user, err := userResource(&zoom.User{
		ID:   "user-id",
		Type: 99,
	}, nil)
	require.NoError(t, err)

	builder := &userResourceType{
		syncResourceTypes: map[string]struct{}{
			resourceTypeLicense.Id: {},
		},
	}
	grants, results, err := builder.Grants(t.Context(), user, resource.SyncOpAttrs{})
	require.NoError(t, err)
	assert.Empty(t, grants)
	assert.Nil(t, results)
}
