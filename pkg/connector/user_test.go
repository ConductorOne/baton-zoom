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
	"github.com/conductorone/baton-zoom/pkg/zoom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

func newZoomTestClient(t *testing.T, httpClient *http.Client, baseURL string) *zoom.Client {
	t.Helper()
	client, err := zoom.NewClient(t.Context(), httpClient, "test-token", baseURL)
	require.NoError(t, err)
	return client
}

func TestUserListRejectsMalformedPageToken(t *testing.T) {
	builder := &userResourceType{}

	_, _, err := builder.List(t.Context(), nil, resource.SyncOpAttrs{
		PageToken: pagination.Token{Token: "{"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Contains(t, err.Error(), "baton-zoom: list users: invalid page token")

	var syntaxErr *json.SyntaxError
	assert.True(t, errors.As(err, &syntaxErr))
}

func TestUserListAcceptsEmptyPageToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/users", r.URL.Path)
		assert.Empty(t, r.URL.Query().Get("next_page_token"))
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"users":[],"next_page_token":""}`))
		require.NoError(t, err)
	}))
	t.Cleanup(srv.Close)

	builder := userBuilder(newZoomTestClient(t, srv.Client(), srv.URL), false, nil)
	users, _, err := builder.List(t.Context(), nil, resource.SyncOpAttrs{})
	require.NoError(t, err)
	assert.Empty(t, users)
}

func TestUserDeleteNotFoundClassification(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode codes.Code
	}{
		{
			name: "Zoom user not found is idempotent success",
			body: `{"code":1001,"message":"User not exist."}`,
		},
		{
			name:     "generic 404 remains an error",
			body:     `{"code":2300,"message":"Route not found."}`,
			wantCode: codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/users/user-1", r.URL.Path)
				assert.Equal(t, string(zoom.Delete), r.URL.Query().Get("action"))
				w.WriteHeader(http.StatusNotFound)
				_, err := w.Write([]byte(tt.body))
				assert.NoError(t, err)
			}))
			defer srv.Close()

			user := &userResourceType{client: newZoomTestClient(t, srv.Client(), srv.URL)}
			userID := v2.ResourceId_builder{
				ResourceType: resourceTypeUser.Id,
				Resource:     "user-1",
			}.Build()

			annos, err := user.Delete(t.Context(), userID)
			assert.Empty(t, annos)
			if tt.wantCode != codes.OK {
				require.Error(t, err)
				assert.Equal(t, tt.wantCode, status.Code(err))
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestInviteResourceIsPending(t *testing.T) {
	invite, err := inviteResource(&zoom.User{
		Email:       "pending@example.com",
		DisplayName: "Pending User",
	}, nil)
	require.NoError(t, err)

	require.NotNil(t, resource.GetStatus(invite))
	assert.Equal(t, v2.Status_RESOURCE_STATUS_PENDING, resource.GetStatus(invite).GetStatus())
}

func TestCreateAccountResourceIsPending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/users", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, err := w.Write([]byte(`{
			"id":"user-1",
			"email":"pending@example.com",
			"first_name":"Pending",
			"last_name":"User",
			"type":1
		}`))
		require.NoError(t, err)
	}))
	defer srv.Close()

	profile, err := structpb.NewStruct(map[string]any{
		emailKey:       "pending@example.com",
		firstNameKey:   "Pending",
		lastNameKey:    "User",
		displayNameKey: "Pending User",
	})
	require.NoError(t, err)
	builder := &userResourceType{client: newZoomTestClient(t, srv.Client(), srv.URL)}

	response, _, _, err := builder.CreateAccount(t.Context(), &v2.AccountInfo{Profile: profile}, nil)
	require.NoError(t, err)
	success, ok := response.(*v2.CreateAccountResponse_SuccessResult)
	require.True(t, ok)
	require.NotNil(t, resource.GetStatus(success.Resource))
	assert.Equal(t, v2.Status_RESOURCE_STATUS_PENDING, resource.GetStatus(success.Resource).GetStatus())
}

func TestUserResourceProfileKeys(t *testing.T) {
	user, err := userResource(&zoom.User{
		ID:        "user-1",
		Email:     "user@example.com",
		FirstName: "Test",
		LastName:  "User",
		GroupIDs:  []string{"group-1"},
		RoleID:    "role-1",
		Type:      int(zoom.LicensedUser),
	}, nil)
	require.NoError(t, err)

	// Grants resolves memberships from GET /v2/users/{userId}. The existing
	// license type profile field remains part of the account profile contract.
	profile := resource.GetProfile(user).AsMap()
	assert.Equal(t, map[string]any{
		firstNameKey:       "Test",
		lastNameKey:        "User",
		loginKey:           "user@example.com",
		userIDKey:          "user-1",
		userTypeProfileKey: float64(zoom.LicensedUser),
	}, profile)
}

func TestUserGrantsUseUserLookupAndSyncSelection(t *testing.T) {
	tests := []struct {
		name         string
		selection    map[string]struct{}
		wantIDs      []string
		wantRequests int
	}{
		{
			name: "nil selection syncs every target",
			wantIDs: []string{
				"group:group-1:member:user:user-1",
				"group:group-2:member:user:user-1",
				"role:role-1:member:user:user-1",
				"license:2:assigned:user:user-1",
			},
			wantRequests: 1,
		},
		{
			name:      "empty selection syncs every target",
			selection: map[string]struct{}{},
			wantIDs: []string{
				"group:group-1:member:user:user-1",
				"group:group-2:member:user:user-1",
				"role:role-1:member:user:user-1",
				"license:2:assigned:user:user-1",
			},
			wantRequests: 1,
		},
		{
			name:      "group target only",
			selection: map[string]struct{}{resourceTypeGroup.Id: {}},
			wantIDs: []string{
				"group:group-1:member:user:user-1",
				"group:group-2:member:user:user-1",
			},
			wantRequests: 1,
		},
		{
			name:         "role target only",
			selection:    map[string]struct{}{resourceTypeRole.Id: {}},
			wantIDs:      []string{"role:role-1:member:user:user-1"},
			wantRequests: 1,
		},
		{
			name:         "license target only",
			selection:    map[string]struct{}{resourceTypeLicense.Id: {}},
			wantIDs:      []string{"license:2:assigned:user:user-1"},
			wantRequests: 1,
		},
		{
			name:         "principal only emits no cross-type grants and skips the lookup",
			selection:    map[string]struct{}{resourceTypeUser.Id: {}},
			wantIDs:      []string{},
			wantRequests: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestCount := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount++
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/users/user-1", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write([]byte(`{
					"id":"user-1",
					"group_ids":["group-1","group-2"],
					"role_id":"role-1",
					"type":2
				}`))
				assert.NoError(t, err)
			}))
			t.Cleanup(srv.Close)

			user, err := userResource(&zoom.User{ID: "user-1"}, nil)
			require.NoError(t, err)

			builder := userBuilder(
				newZoomTestClient(t, srv.Client(), srv.URL),
				true,
				tt.selection,
			)
			grants, _, err := builder.Grants(t.Context(), user, resource.SyncOpAttrs{})
			require.NoError(t, err)
			assert.Equal(t, tt.wantRequests, requestCount)

			gotIDs := make([]string, 0, len(grants))
			for _, gotGrant := range grants {
				gotIDs = append(gotIDs, gotGrant.GetId())
				assert.Equal(t, user.GetId(), gotGrant.GetPrincipal().GetId())
			}
			assert.Equal(t, tt.wantIDs, gotIDs)
		})
	}
}

func TestUserGrantsWrapLookupError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, err := w.Write([]byte(`{"code":429,"message":"Too many requests."}`))
		assert.NoError(t, err)
	}))
	t.Cleanup(srv.Close)

	user, err := userResource(&zoom.User{ID: "user-1"}, nil)
	require.NoError(t, err)

	builder := userBuilder(newZoomTestClient(t, srv.Client(), srv.URL), true, nil)
	_, _, err = builder.Grants(t.Context(), user, resource.SyncOpAttrs{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "baton-zoom: list user grants:")
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestNewForCapabilitiesIncludesAllUserGrantTargets(t *testing.T) {
	syncers := NewForCapabilities().ResourceSyncers(t.Context())
	require.NotEmpty(t, syncers)

	user, ok := syncers[0].(*userResourceType)
	require.True(t, ok)
	assert.True(t, willSyncResourceType(user.syncResourceTypes, resourceTypeGroup.Id))
	assert.True(t, willSyncResourceType(user.syncResourceTypes, resourceTypeRole.Id))
	assert.True(t, willSyncResourceType(user.syncResourceTypes, resourceTypeLicense.Id))
}

// Zoom answers 409/1005 when the email is already on the account. The conflict
// itself proves the account exists, so C1 gets an already-exists result rather
// than a failed provisioning task.
func TestCreateAccountOnDuplicateEmail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/users", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, err := w.Write([]byte(`{"code":1005,"message":"User already in the account: taken@example.com"}`))
		require.NoError(t, err)
	}))
	defer srv.Close()

	profile, err := structpb.NewStruct(map[string]any{
		emailKey:       "taken@example.com",
		firstNameKey:   "Taken",
		lastNameKey:    "User",
		displayNameKey: "Taken User",
	})
	require.NoError(t, err)
	builder := &userResourceType{client: newZoomTestClient(t, srv.Client(), srv.URL)}

	response, _, _, err := builder.CreateAccount(t.Context(), &v2.AccountInfo{Profile: profile}, nil)
	require.NoError(t, err)
	alreadyExists, ok := response.(*v2.CreateAccountResponse_AlreadyExistsResult)
	require.True(t, ok)
	assert.True(t, alreadyExists.IsCreateAccountResult)
}

func TestRequiredStringProfileField(t *testing.T) {
	tests := []struct {
		name        string
		profile     map[string]any
		want        string
		wantMessage string
	}{
		{
			name:        "missing field",
			profile:     map[string]any{},
			wantMessage: "baton-zoom: create account: email is required",
		},
		{
			name:        "empty field",
			profile:     map[string]any{emailKey: ""},
			wantMessage: "baton-zoom: create account: email is required",
		},
		{
			name:        "wrong field type",
			profile:     map[string]any{emailKey: true},
			wantMessage: "baton-zoom: create account: invalid email format: expected a string",
		},
		{
			name:    "valid field",
			profile: map[string]any{emailKey: "user@example.com"},
			want:    "user@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := requiredStringProfileField(tt.profile, emailKey, "email")
			if tt.wantMessage == "" {
				require.NoError(t, err)
				assert.Equal(t, tt.want, value)
				return
			}

			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
			assert.Contains(t, err.Error(), tt.wantMessage)
		})
	}
}
