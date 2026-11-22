package connector

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-zoom/pkg/zoom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ connectorbuilder.ResourceDeleterLimited = (*inviteResourceType)(nil)

func TestInviteDelete(t *testing.T) {
	tests := []struct {
		name         string
		userStatus   string
		getStatus    int
		getError     int
		deleteStatus int
		deleteError  int
		wantDelete   bool
		wantError    bool
		wantCode     codes.Code
	}{
		{name: "pending", userStatus: userStatusPending, wantDelete: true},
		{name: "active", userStatus: "active", wantError: true, wantCode: codes.FailedPrecondition},
		{name: "inactive", userStatus: "inactive", wantError: true, wantCode: codes.FailedPrecondition},
		{name: "missing status", wantError: true, wantCode: codes.FailedPrecondition},
		{name: "unknown status", userStatus: "unknown", wantError: true, wantCode: codes.FailedPrecondition},
		{name: "already gone", getStatus: http.StatusNotFound, getError: zoom.UserNotFoundErrorCode},
		{name: "gone after preflight", userStatus: userStatusPending, deleteStatus: http.StatusNotFound, deleteError: zoom.UserNotFoundErrorCode, wantDelete: true},
		{name: "get API error", getStatus: http.StatusForbidden, getError: 200, wantError: true},
		{name: "get unrelated 404", getStatus: http.StatusNotFound, getError: 200, wantError: true},
		{name: "delete API error", userStatus: userStatusPending, deleteStatus: http.StatusForbidden, deleteError: 200, wantDelete: true, wantError: true},
		{name: "delete unrelated 404", userStatus: userStatusPending, deleteStatus: http.StatusNotFound, deleteError: 200, wantDelete: true, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const email = "invite+test@example.com"
			var methods []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				methods = append(methods, r.Method)
				assert.Equal(t, "/v2/users/"+email, r.URL.Path)
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				w.Header().Set("Content-Type", "application/json")
				switch r.Method {
				case http.MethodGet:
					assert.Empty(t, r.URL.RawQuery)
					w.Header().Set("X-RateLimit-Limit", "100")
					w.Header().Set("X-RateLimit-Remaining", "99")
					if tt.getStatus != 0 {
						w.WriteHeader(tt.getStatus)
						assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"code": tt.getError, "message": "get failed"}))
						return
					}
					assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"email": email, "status": tt.userStatus}))
				case http.MethodDelete:
					assert.Equal(t, "action=disassociate", r.URL.RawQuery)
					if tt.deleteStatus != 0 {
						w.WriteHeader(tt.deleteStatus)
						assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"code": tt.deleteError, "message": "delete failed"}))
						return
					}
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected method %s", r.Method)
					w.WriteHeader(http.StatusMethodNotAllowed)
				}
			}))
			defer server.Close()

			builder := inviteBuilder(newZoomTestClient(t, server.Client(), server.URL+"/v2"))
			annos, err := builder.Delete(t.Context(), &v2.ResourceId{ResourceType: resourceTypeInvite.Id, Resource: email})
			if tt.wantError {
				require.Error(t, err)
				assert.NotContains(t, err.Error(), email)
				if tt.wantCode != codes.OK {
					assert.Equal(t, tt.wantCode, status.Code(err))
					assert.Contains(t, err.Error(), "refusing to disassociate")
				}
			} else {
				require.NoError(t, err)
			}
			wantMethods := []string{http.MethodGet}
			if tt.wantDelete {
				wantMethods = append(wantMethods, http.MethodDelete)
			}
			assert.Equal(t, wantMethods, methods)
			rateLimit := &v2.RateLimitDescription{}
			found, err := annos.Pick(rateLimit)
			require.NoError(t, err)
			require.True(t, found)
			assert.EqualValues(t, 100, rateLimit.GetLimit())
		})
	}
}

func TestInviteDeleteRejectsInvalidResource(t *testing.T) {
	for _, id := range []*v2.ResourceId{
		nil,
		{ResourceType: resourceTypeInvite.Id},
		{ResourceType: resourceTypeUser.Id, Resource: "user-id"},
	} {
		_, err := inviteBuilder(nil).Delete(t.Context(), id)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	}
}

func TestInviteDeletePermissions(t *testing.T) {
	perms := &v2.CapabilityPermissions{}
	annos := annotations.Annotations(resourceTypeInvite.Annotations)
	found, err := annos.Pick(perms)
	require.NoError(t, err)
	require.True(t, found)
	var scopes []string
	for _, permission := range perms.GetPermissions() {
		scopes = append(scopes, permission.GetPermission())
	}
	assert.ElementsMatch(t, []string{scopeUserReadList, scopeUserRead, scopeUserDelete}, scopes)
}

func TestUserDeleteStillPermanentlyDeletes(t *testing.T) {
	for _, responseStatus := range []int{http.StatusNoContent, http.StatusNotFound, http.StatusForbidden} {
		t.Run(http.StatusText(responseStatus), func(t *testing.T) {
			var methods []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				methods = append(methods, r.Method)
				assert.Equal(t, "/v2/users/user-id", r.URL.Path)
				assert.Equal(t, "action=delete", r.URL.RawQuery)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(responseStatus)
				if responseStatus != http.StatusNoContent {
					assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"code": zoom.UserNotFoundErrorCode, "message": "user not found"}))
				}
			}))
			defer server.Close()
			builder := userBuilder(newZoomTestClient(t, server.Client(), server.URL+"/v2"), false, nil)
			_, err := builder.Delete(t.Context(), &v2.ResourceId{ResourceType: resourceTypeUser.Id, Resource: "user-id"})
			if responseStatus == http.StatusForbidden {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, []string{http.MethodDelete}, methods)
		})
	}
}
