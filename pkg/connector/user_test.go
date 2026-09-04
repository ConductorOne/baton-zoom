package connector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-zoom/pkg/zoom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newZoomTestClient(t *testing.T, httpClient *http.Client, baseURL string) *zoom.Client {
	t.Helper()
	client, err := zoom.NewClient(t.Context(), httpClient, "test-token", baseURL)
	require.NoError(t, err)
	return client
}

func TestUserDeleteNotFoundClassification(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name: "Zoom user not found is idempotent success",
			body: `{"code":1001,"message":"User not exist."}`,
		},
		{
			name:    "generic 404 remains an error",
			body:    `{"code":2300,"message":"Route not found."}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/users/user-1", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
				_, err := w.Write([]byte(tt.body))
				assert.NoError(t, err)
			}))
			defer srv.Close()

			user := &userResourceType{
				client: newZoomTestClient(t, srv.Client(), srv.URL),
			}
			userID := v2.ResourceId_builder{
				ResourceType: resourceTypeUser.Id,
				Resource:     "user-1",
			}.Build()

			annos, err := user.Delete(context.Background(), userID)
			assert.Empty(t, annos)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
