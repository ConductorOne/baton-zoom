package connector

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-zoom/pkg/zoom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func licenseProvisioningObjects(licenseType zoom.UserType) (*v2.Resource, *v2.Entitlement) {
	licenseID := strconv.Itoa(int(licenseType))
	principal := v2.Resource_builder{
		Id: v2.ResourceId_builder{
			ResourceType: resourceTypeUser.Id,
			Resource:     "user-1",
		}.Build(),
	}.Build()
	license := v2.Resource_builder{
		Id: v2.ResourceId_builder{
			ResourceType: resourceTypeLicense.Id,
			Resource:     licenseID,
		}.Build(),
	}.Build()
	entitlement := v2.Entitlement_builder{
		Resource: license,
		Id:       "license:" + licenseID + ":" + assignedEntitlement,
		Slug:     assignedEntitlement,
	}.Build()
	return principal, entitlement
}

func TestLicenseGrantReturnsRequestedGrant(t *testing.T) {
	tests := []struct {
		name        string
		currentType zoom.UserType
		wantPatch   bool
	}{
		{name: "new license assignment", currentType: zoom.BasicUser, wantPatch: true},
		{name: "license already assigned", currentType: zoom.LicensedUser},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patchCalled := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/users/user-1":
					w.Header().Set("Content-Type", "application/json")
					_, err := w.Write([]byte(`{"id":"user-1","type":` + strconv.Itoa(int(tt.currentType)) + `}`))
					require.NoError(t, err)
				case r.Method == http.MethodPatch && r.URL.Path == "/users/user-1":
					patchCalled = true
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(srv.Close)

			principal, entitlement := licenseProvisioningObjects(zoom.LicensedUser)
			builder := licenseBuilder(newZoomTestClient(t, srv.Client(), srv.URL))

			grants, annos, err := builder.Grant(t.Context(), principal, entitlement)
			require.NoError(t, err)
			require.Len(t, grants, 1)
			assert.Equal(t, entitlement.GetId(), grants[0].GetEntitlement().GetId())
			assert.Equal(t, principal.GetId(), grants[0].GetPrincipal().GetId())
			assert.Equal(t, tt.wantPatch, patchCalled)
			assert.Equal(t, !tt.wantPatch, (&annos).Contains(&v2.GrantAlreadyExists{}))
		})
	}
}

// A Zoom user holds exactly one type, so assigning a tier drops the previous
// one. C1 only removes the sibling grant when the entitlement declares an
// exclusion group and the Grant reports which grant it replaced.
func TestLicenseTiersAreMutuallyExclusive(t *testing.T) {
	license := v2.Resource_builder{
		Id: v2.ResourceId_builder{
			ResourceType: resourceTypeLicense.Id,
			Resource:     strconv.Itoa(int(zoom.LicensedUser)),
		}.Build(),
		DisplayName: "Licensed",
	}.Build()
	builder := licenseBuilder(nil)

	entitlements, _, err := builder.Entitlements(t.Context(), license, resource.SyncOpAttrs{})
	require.NoError(t, err)
	require.Len(t, entitlements, 1)

	annos := annotations.Annotations(entitlements[0].GetAnnotations())
	exclusion := &v2.EntitlementExclusionGroup{}
	ok, err := annos.Pick(exclusion)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, licenseExclusionGroup, exclusion.GetExclusionGroupId())
}

func TestLicenseGrantReplacesPreviousTier(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/users/user-1":
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"id":"user-1","type":1}`))
			require.NoError(t, err)
		case r.Method == http.MethodPatch && r.URL.Path == "/users/user-1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	principal, entitlement := licenseProvisioningObjects(zoom.LicensedUser)
	builder := licenseBuilder(newZoomTestClient(t, srv.Client(), srv.URL))

	_, annos, err := builder.Grant(t.Context(), principal, entitlement)
	require.NoError(t, err)

	replaced := &v2.GrantReplaced{}
	ok, err := annos.Pick(replaced)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "license:1:assigned:user:user-1", replaced.GetReplacedGrantId())
}
