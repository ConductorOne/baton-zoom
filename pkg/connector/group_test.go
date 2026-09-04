package connector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func groupProvisioningObjects() (*v2.Resource, *v2.Entitlement, *v2.Grant) {
	return groupProvisioningObjectsForSlug(memberEntitlement)
}

func groupProvisioningObjectsForSlug(slug string) (*v2.Resource, *v2.Entitlement, *v2.Grant) {
	principalID := v2.ResourceId_builder{
		ResourceType: resourceTypeUser.Id,
		Resource:     "user-1",
	}.Build()
	principal := v2.Resource_builder{Id: principalID}.Build()
	group := v2.Resource_builder{
		Id: v2.ResourceId_builder{
			ResourceType: resourceTypeGroup.Id,
			Resource:     "group-1",
		}.Build(),
	}.Build()
	entitlement := v2.Entitlement_builder{
		Resource: group,
		Id:       "group:group-1:" + slug,
		Slug:     slug,
	}.Build()
	grant := v2.Grant_builder{
		Id:          "group:group-1:" + slug + ":user:user-1",
		Entitlement: entitlement,
		Principal:   principal,
	}.Build()
	return principal, entitlement, grant
}

func TestGroupGrantReturnsRequestedGrant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/groups/group-1/members", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	principal, entitlement, _ := groupProvisioningObjects()
	group := groupBuilder(newZoomTestClient(t, srv.Client(), srv.URL))

	grants, annos, err := group.Grant(context.Background(), principal, entitlement)
	require.NoError(t, err)
	assert.Empty(t, annos)
	require.Len(t, grants, 1)
	assert.Equal(t, entitlement.GetId(), grants[0].GetEntitlement().GetId())
	assert.Equal(t, principal.GetId(), grants[0].GetPrincipal().GetId())
}

func TestGroupRevokeMissingMemberIsIdempotent(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name: "Zoom group member not found",
			body: `{"code":4131,"message":"Group member does not exist."}`,
		},
		{
			name:    "generic 404",
			body:    `{"code":1001,"message":"Route not found."}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/groups/group-1/members/user-1", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
				_, err := w.Write([]byte(tt.body))
				assert.NoError(t, err)
			}))
			defer srv.Close()

			_, _, grant := groupProvisioningObjects()
			group := groupBuilder(newZoomTestClient(t, srv.Client(), srv.URL))

			annos, err := group.Revoke(context.Background(), grant)
			if tt.wantErr {
				require.Error(t, err)
				assert.Empty(t, annos)
				return
			}

			require.NoError(t, err)
			assert.True(t, (&annos).Contains(&v2.GrantAlreadyRevoked{}))
		})
	}
}

func TestGroupRevokeMissingAdminIsIdempotent(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
	}{
		{
			name:       "Zoom user is not a group administrator",
			statusCode: http.StatusBadRequest,
			body:       `{"code":4138,"message":"That user is not an administrator for the group: \"group-1\"."}`,
		},
		{
			name:       "paid-account denial stays an error",
			statusCode: http.StatusBadRequest,
			body:       `{"code":200,"message":"Only available for Paid account."}`,
			wantErr:    true,
		},
		{
			name:       "missing group stays an error",
			statusCode: http.StatusNotFound,
			body:       `{"code":4130,"message":"A group with the group-1 ID does not exist."}`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/groups/group-1/admins/user-1", r.URL.Path)
				w.WriteHeader(tt.statusCode)
				_, err := w.Write([]byte(tt.body))
				assert.NoError(t, err)
			}))
			defer srv.Close()

			_, _, grant := groupProvisioningObjectsForSlug(adminEntitlement)
			group := groupBuilder(newZoomTestClient(t, srv.Client(), srv.URL))

			annos, err := group.Revoke(context.Background(), grant)
			if tt.wantErr {
				require.Error(t, err)
				assert.Empty(t, annos)
				return
			}

			require.NoError(t, err)
			assert.True(t, (&annos).Contains(&v2.GrantAlreadyRevoked{}))
		})
	}
}
