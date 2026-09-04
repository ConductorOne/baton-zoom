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

func TestRoleGrantReturnsRequestedGrant(t *testing.T) {
	tests := []struct {
		name        string
		currentRole string
		wantAssign  bool
	}{
		{name: "new role assignment", currentRole: "role-old", wantAssign: true},
		{name: "role already assigned", currentRole: "role-target"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assignCalled := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/users/user-1":
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

			principal := v2.Resource_builder{
				Id: v2.ResourceId_builder{
					ResourceType: resourceTypeUser.Id,
					Resource:     "user-1",
				}.Build(),
			}.Build()
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
			role := roleBuilder(newZoomTestClient(t, srv.Client(), srv.URL))

			grants, annos, err := role.Grant(context.Background(), principal, entitlement)
			require.NoError(t, err)
			require.Len(t, grants, 1)
			assert.Equal(t, entitlement.GetId(), grants[0].GetEntitlement().GetId())
			assert.Equal(t, principal.GetId(), grants[0].GetPrincipal().GetId())
			assert.Equal(t, tt.wantAssign, assignCalled)
			assert.Equal(t, !tt.wantAssign, (&annos).Contains(&v2.GrantAlreadyExists{}))
		})
	}
}
