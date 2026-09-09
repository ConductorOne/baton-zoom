package connector

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func groupProvisioningObjects(t *testing.T, slug string) (*v2.Resource, *v2.Entitlement, *v2.Grant) {
	t.Helper()
	principal, err := resource.NewUserResource(
		"User",
		resourceTypeUser,
		"user-1",
		[]resource.UserTraitOption{resource.WithEmail("user-1@example.com", true)},
	)
	require.NoError(t, err)
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

func TestGroupListPageTokenValidation(t *testing.T) {
	builder := &groupResourceType{}

	_, _, err := builder.List(t.Context(), nil, resource.SyncOpAttrs{
		PageToken: pagination.Token{Token: "{"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Contains(t, err.Error(), "baton-zoom: list groups: invalid page token")

	var syntaxErr *json.SyntaxError
	assert.True(t, errors.As(err, &syntaxErr))

	bag, page, err := parsePageToken("", &v2.ResourceId{ResourceType: resourceTypeGroup.Id}, "list groups")
	require.NoError(t, err)
	require.NotNil(t, bag)
	assert.Empty(t, page)
}

func TestGroupGrantReturnsRequestedGrant(t *testing.T) {
	tests := []struct {
		name     string
		slug     string
		wantPath string
	}{
		{name: "member", slug: memberEntitlement, wantPath: "/groups/group-1/members"},
		{name: "admin", slug: adminEntitlement, wantPath: "/groups/group-1/admins"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, tt.wantPath, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, err := w.Write([]byte(`{"ids":"user-1","added_at":"2026-09-08T18:19:46Z"}`))
				assert.NoError(t, err)
			}))
			defer srv.Close()

			principal, entitlement, _ := groupProvisioningObjects(t, tt.slug)
			group := groupBuilder(newZoomTestClient(t, srv.Client(), srv.URL))

			grants, annos, err := group.Grant(t.Context(), principal, entitlement)
			require.NoError(t, err)
			assert.False(t, (&annos).Contains(&v2.GrantAlreadyExists{}))
			require.Len(t, grants, 1)
			assert.Equal(t, entitlement.GetId(), grants[0].GetEntitlement().GetId())
			assert.Equal(t, principal.GetId(), grants[0].GetPrincipal().GetId())
		})
	}
}

func TestGroupGrantsEmitOnlyAdminsWithPagination(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/groups/group-1/admins", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("next_page_token") {
		case "":
			_, err := w.Write([]byte(`{"admins":[{"id":"admin-1","email":"admin-1@example.com"}],"next_page_token":"page-2"}`))
			require.NoError(t, err)
		case "page-2":
			_, err := w.Write([]byte(`{"admins":[{"id":"admin-2","email":"admin-2@example.com"}],"next_page_token":""}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected next_page_token %q", r.URL.Query().Get("next_page_token"))
		}
	}))
	t.Cleanup(srv.Close)

	group := v2.Resource_builder{
		Id: v2.ResourceId_builder{
			ResourceType: resourceTypeGroup.Id,
			Resource:     "group-1",
		}.Build(),
		DisplayName: "Group 1",
	}.Build()
	builder := groupBuilder(newZoomTestClient(t, srv.Client(), srv.URL))

	first, firstResults, err := builder.Grants(t.Context(), group, resource.SyncOpAttrs{})
	require.NoError(t, err)
	require.Len(t, first, 1)
	assert.Equal(t, "group:group-1:admin:user:admin-1", first[0].GetId())
	assert.Equal(t, "group:group-1:admin", first[0].GetEntitlement().GetId())
	assert.Equal(t, "user:admin-1", first[0].GetPrincipal().GetId().GetResourceType()+":"+first[0].GetPrincipal().GetId().GetResource())
	require.NotEmpty(t, firstResults.NextPageToken)

	second, secondResults, err := builder.Grants(t.Context(), group, resource.SyncOpAttrs{
		PageToken: pagination.Token{Token: firstResults.NextPageToken},
	})
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.Equal(t, "group:group-1:admin:user:admin-2", second[0].GetId())
	assert.Equal(t, "group:group-1:admin", second[0].GetEntitlement().GetId())
	assert.Equal(t, "user:admin-2", second[0].GetPrincipal().GetId().GetResourceType()+":"+second[0].GetPrincipal().GetId().GetResource())
	assert.Empty(t, secondResults.NextPageToken)
	assert.Equal(t, 2, requests)
}

// Zoom returns the same 201 with an empty ids field whether the user was
// already there or was silently ignored (a pending user is), so the connector
// reads the membership back: present is an idempotent grant, absent is a
// failure, never a grant C1 would show for access that does not exist.
func TestGroupGrantResolvesEmptyZoomResponse(t *testing.T) {
	tests := []struct {
		name       string
		slug       string
		listPath   string
		listBody   string
		wantErr    bool
		wantExists bool
	}{
		{
			name:       "member is already in the group",
			slug:       memberEntitlement,
			listPath:   "/users/user-1",
			listBody:   `{"id":"user-1","group_ids":["group-1"]}`,
			wantExists: true,
		},
		{
			name:     "member was silently not added",
			slug:     memberEntitlement,
			listPath: "/users/user-1",
			listBody: `{"id":"user-1","group_ids":[]}`,
			wantErr:  true,
		},
		{
			name:       "user is already a group admin",
			slug:       adminEntitlement,
			listPath:   "/groups/group-1/admins",
			listBody:   `{"admins":[{"email":"user-1@example.com","name":"User"}],"next_page_token":""}`,
			wantExists: true,
		},
		{
			name:     "admin was silently not added",
			slug:     adminEntitlement,
			listPath: "/groups/group-1/admins",
			listBody: `{"admins":[],"next_page_token":""}`,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.Method {
				case http.MethodPost:
					w.WriteHeader(http.StatusCreated)
					_, err := w.Write([]byte(`{"ids":"","added_at":"2026-09-08T18:19:46Z"}`))
					assert.NoError(t, err)
				case http.MethodGet:
					assert.Equal(t, tt.listPath, r.URL.Path)
					_, err := w.Write([]byte(tt.listBody))
					assert.NoError(t, err)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
			}))
			defer srv.Close()

			principal, entitlement, _ := groupProvisioningObjects(t, tt.slug)
			group := groupBuilder(newZoomTestClient(t, srv.Client(), srv.URL))

			grants, annos, err := group.Grant(t.Context(), principal, entitlement)
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, codes.FailedPrecondition, status.Code(err))
				assert.Empty(t, grants)
				return
			}
			require.NoError(t, err)
			require.Len(t, grants, 1)
			assert.Equal(t, tt.wantExists, (&annos).Contains(&v2.GrantAlreadyExists{}))
		})
	}
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
			name:    "missing group remains an error",
			body:    `{"code":4130,"message":"A group with the group-1 ID does not exist."}`,
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

			_, _, grant := groupProvisioningObjects(t, memberEntitlement)
			group := groupBuilder(newZoomTestClient(t, srv.Client(), srv.URL))

			annos, err := group.Revoke(t.Context(), grant)
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
		name    string
		body    string
		wantErr bool
	}{
		{
			name: "Zoom user is not a group administrator",
			body: `{"code":4138,"message":"That user is not an administrator for the group: \"group-1\"."}`,
		},
		{
			name:    "paid-account denial remains an error",
			body:    `{"code":200,"message":"Only available for Paid account."}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/groups/group-1/admins/user-1", r.URL.Path)
				w.WriteHeader(http.StatusBadRequest)
				_, err := w.Write([]byte(tt.body))
				assert.NoError(t, err)
			}))
			defer srv.Close()

			_, _, grant := groupProvisioningObjects(t, adminEntitlement)
			group := groupBuilder(newZoomTestClient(t, srv.Client(), srv.URL))

			annos, err := group.Revoke(t.Context(), grant)
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

func TestGroupGrantRejectsUnknownEntitlement(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	principal, entitlement, grant := groupProvisioningObjects(t, "owner")
	group := groupBuilder(newZoomTestClient(t, srv.Client(), srv.URL))

	_, _, err := group.Grant(t.Context(), principal, entitlement)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = group.Revoke(t.Context(), grant)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}
