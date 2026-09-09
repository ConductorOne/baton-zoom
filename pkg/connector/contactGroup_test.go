package connector

import (
	"net/http"
	"net/http/httptest"
	"testing"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContactGroupGroupGrantIsExpandableAndImmutable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/contacts/groups/contact-group-1/members", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"group_members":[{"id":"group-1","name":"Engineering","type":2}]}`))
		require.NoError(t, err)
	}))
	defer srv.Close()

	contactGroup := v2.Resource_builder{
		Id: v2.ResourceId_builder{
			ResourceType: resourceTypeContactGroup.Id,
			Resource:     "contact-group-1",
		}.Build(),
		DisplayName: "Contacts",
	}.Build()
	builder := contactGroupBuilder(newZoomTestClient(t, srv.Client(), srv.URL), nil)

	grants, _, err := builder.Grants(t.Context(), contactGroup, resource.SyncOpAttrs{
		PageToken: pagination.Token{},
	})
	require.NoError(t, err)
	require.Len(t, grants, 1)

	annos := annotations.Annotations(grants[0].GetAnnotations())
	expandable := &v2.GrantExpandable{}
	ok, err := annos.Pick(expandable)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{"group:group-1:member"}, expandable.GetEntitlementIds())
	assert.True(t, expandable.GetShallow())
	assert.Equal(t, []string{resourceTypeUser.Id}, expandable.GetResourceTypeIds())

	immutable := &v2.GrantImmutable{}
	ok, err = annos.Pick(immutable)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestContactGroupGrantsRespectPrincipalSyncSelection(t *testing.T) {
	tests := []struct {
		name               string
		selection          map[string]struct{}
		wantPrincipalTypes []string
	}{
		{
			name:               "nil selection emits users and groups",
			wantPrincipalTypes: []string{resourceTypeUser.Id, resourceTypeGroup.Id},
		},
		{
			name:               "empty selection emits users and groups",
			selection:          map[string]struct{}{},
			wantPrincipalTypes: []string{resourceTypeUser.Id, resourceTypeGroup.Id},
		},
		{
			name: "user selection emits only users",
			selection: map[string]struct{}{
				resourceTypeUser.Id:         {},
				resourceTypeContactGroup.Id: {},
			},
			wantPrincipalTypes: []string{resourceTypeUser.Id},
		},
		{
			name: "group selection emits only groups",
			selection: map[string]struct{}{
				resourceTypeGroup.Id:        {},
				resourceTypeContactGroup.Id: {},
			},
			wantPrincipalTypes: []string{resourceTypeGroup.Id},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/contacts/groups/contact-group-1/members", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write([]byte(`{
					"group_members":[
						{"id":"user-1","name":"User","type":1},
						{"id":"group-1","name":"Group","type":2}
					]
				}`))
				require.NoError(t, err)
			}))
			t.Cleanup(srv.Close)

			contactGroup := v2.Resource_builder{
				Id: v2.ResourceId_builder{
					ResourceType: resourceTypeContactGroup.Id,
					Resource:     "contact-group-1",
				}.Build(),
				DisplayName: "Contacts",
			}.Build()
			builder := contactGroupBuilder(newZoomTestClient(t, srv.Client(), srv.URL), tt.selection)

			grants, _, err := builder.Grants(t.Context(), contactGroup, resource.SyncOpAttrs{})
			require.NoError(t, err)
			require.Len(t, grants, len(tt.wantPrincipalTypes))
			for i, gotGrant := range grants {
				assert.Equal(t, resourceTypeContactGroup.Id, gotGrant.GetEntitlement().GetResource().GetId().GetResourceType())
				assert.Equal(t, "contact-group-1", gotGrant.GetEntitlement().GetResource().GetId().GetResource())
				assert.Equal(t, tt.wantPrincipalTypes[i], gotGrant.GetPrincipal().GetId().GetResourceType())
				assert.True(t, willSyncResourceType(tt.selection, gotGrant.GetPrincipal().GetId().GetResourceType()))
			}
		})
	}
}
