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
	builder := contactGroupBuilder(newZoomTestClient(t, srv.Client(), srv.URL))

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
