package connector

import (
	"net/http"
	"net/http/httptest"
	"testing"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func roleProvisioningObjects() (*v2.Resource, *v2.Entitlement, *v2.Grant) {
	principalID := v2.ResourceId_builder{
		ResourceType: resourceTypeUser.Id,
		Resource:     "user-1",
	}.Build()
	principal := v2.Resource_builder{Id: principalID}.Build()
	roleResource := v2.Resource_builder{
		Id: v2.ResourceId_builder{
			ResourceType: resourceTypeRole.Id,
			Resource:     "role-target",
		}.Build(),
	}.Build()
	entitlement := v2.Entitlement_builder{
		Resource: roleResource,
		Id:       "role:role-target:member",
		Slug:     memberEntitlement,
	}.Build()
	grant := v2.Grant_builder{
		Id:          "role:role-target:member:user:user-1",
		Entitlement: entitlement,
		Principal:   principal,
	}.Build()
	return principal, entitlement, grant
}

func TestRoleEntitlementIsMutuallyExclusive(t *testing.T) {
	roleResource := v2.Resource_builder{
		Id: v2.ResourceId_builder{
			ResourceType: resourceTypeRole.Id,
			Resource:     "role-1",
		}.Build(),
	}.Build()
	builder := roleBuilder(nil)

	entitlements, _, err := builder.Entitlements(t.Context(), roleResource, resource.SyncOpAttrs{})
	require.NoError(t, err)
	require.Len(t, entitlements, 1)

	exclusionGroup := &v2.EntitlementExclusionGroup{}
	annos := annotations.Annotations(entitlements[0].GetAnnotations())
	ok, err := annos.Pick(exclusionGroup)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, roleExclusionGroup, exclusionGroup.GetExclusionGroupId())
}

func TestRoleGrantsAreEmittedFromUsers(t *testing.T) {
	builder := roleBuilder(nil)
	role := v2.Resource_builder{
		Id: v2.ResourceId_builder{
			ResourceType: resourceTypeRole.Id,
			Resource:     "role-1",
		}.Build(),
	}.Build()

	grants, results, err := builder.Grants(t.Context(), role, resource.SyncOpAttrs{})
	require.NoError(t, err)
	assert.Empty(t, grants)
	assert.Nil(t, results)

	skipGrants := &v2.SkipGrants{}
	resourceTypeAnnos := annotations.Annotations(builder.ResourceType(t.Context()).GetAnnotations())
	ok, err := resourceTypeAnnos.Pick(skipGrants)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestRoleGrantReturnsRequestedGrant(t *testing.T) {
	tests := []struct {
		name              string
		currentRole       string
		wantAssign        bool
		wantReplacedGrant string
	}{
		{
			name:              "new role assignment",
			currentRole:       "role-old",
			wantAssign:        true,
			wantReplacedGrant: "role:role-old:member:user:user-1",
		},
		{name: "role already assigned", currentRole: "role-target"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assignCalled := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/users/user-1":
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, err := w.Write([]byte(`{"id":"user-1","status":"active","role_id":"` + tt.currentRole + `"}`))
					assert.NoError(t, err)
				case r.Method == http.MethodPost && r.URL.Path == "/roles/role-target/members":
					assignCalled = true
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()

			principal, entitlement, _ := roleProvisioningObjects()
			role := roleBuilder(newZoomTestClient(t, srv.Client(), srv.URL))

			grants, annos, err := role.Grant(t.Context(), principal, entitlement)
			require.NoError(t, err)
			require.Len(t, grants, 1)
			assert.Equal(t, entitlement.GetId(), grants[0].GetEntitlement().GetId())
			assert.Equal(t, principal.GetId(), grants[0].GetPrincipal().GetId())
			assert.Equal(t, tt.wantAssign, assignCalled)
			assert.Equal(t, !tt.wantAssign, (&annos).Contains(&v2.GrantAlreadyExists{}))

			replaced := &v2.GrantReplaced{}
			ok, err := annos.Pick(replaced)
			require.NoError(t, err)
			assert.Equal(t, tt.wantReplacedGrant != "", ok)
			assert.Equal(t, tt.wantReplacedGrant, replaced.GetReplacedGrantId())
		})
	}
}

func TestRoleRevokeIsIdempotent(t *testing.T) {
	tests := []struct {
		name         string
		currentRole  string
		wantUnassign bool
	}{
		{name: "assigned role is removed", currentRole: "role-target", wantUnassign: true},
		{name: "different role is already revoked", currentRole: "role-other"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unassignCalled := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/users/user-1":
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, err := w.Write([]byte(`{"id":"user-1","status":"active","role_id":"` + tt.currentRole + `"}`))
					assert.NoError(t, err)
				case r.Method == http.MethodDelete && r.URL.Path == "/roles/role-target/members/user-1":
					unassignCalled = true
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()

			_, _, grant := roleProvisioningObjects()
			role := roleBuilder(newZoomTestClient(t, srv.Client(), srv.URL))

			annos, err := role.Revoke(t.Context(), grant)
			require.NoError(t, err)
			assert.Equal(t, tt.wantUnassign, unassignCalled)
			assert.Equal(t, !tt.wantUnassign, (&annos).Contains(&v2.GrantAlreadyRevoked{}))
		})
	}
}
