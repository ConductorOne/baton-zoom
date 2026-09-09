package connector

import (
	"net/http"
	"net/http/httptest"
	"testing"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
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
