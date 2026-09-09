package connector

import (
	"context"
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

func TestConnectorClientErrorsIncludeOperationContext(t *testing.T) {
	type runFunc func(context.Context, *zoom.Client) (*resource.SyncOpResults, error)
	tests := []struct {
		name        string
		wantContext string
		run         runFunc
	}{
		{
			name:        "user list",
			wantContext: "baton-zoom: list users:",
			run: func(ctx context.Context, client *zoom.Client) (*resource.SyncOpResults, error) {
				_, results, err := userBuilder(client, false, nil).List(ctx, nil, resource.SyncOpAttrs{})
				return results, err
			},
		},
		{
			name:        "group list",
			wantContext: "baton-zoom: list groups:",
			run: func(ctx context.Context, client *zoom.Client) (*resource.SyncOpResults, error) {
				_, results, err := groupBuilder(client).List(ctx, nil, resource.SyncOpAttrs{})
				return results, err
			},
		},
		{
			name:        "group grants",
			wantContext: "baton-zoom: list administrators for group group-1:",
			run: func(ctx context.Context, client *zoom.Client) (*resource.SyncOpResults, error) {
				group := v2.Resource_builder{
					Id: v2.ResourceId_builder{ResourceType: resourceTypeGroup.Id, Resource: "group-1"}.Build(),
				}.Build()
				_, results, err := groupBuilder(client).Grants(ctx, group, resource.SyncOpAttrs{})
				return results, err
			},
		},
		{
			name:        "role list",
			wantContext: "baton-zoom: list roles:",
			run: func(ctx context.Context, client *zoom.Client) (*resource.SyncOpResults, error) {
				_, results, err := roleBuilder(client).List(ctx, nil, resource.SyncOpAttrs{})
				return results, err
			},
		},
		{
			name:        "contact group list",
			wantContext: "baton-zoom: list contact groups:",
			run: func(ctx context.Context, client *zoom.Client) (*resource.SyncOpResults, error) {
				_, results, err := contactGroupBuilder(client, nil).List(ctx, nil, resource.SyncOpAttrs{})
				return results, err
			},
		},
		{
			name:        "contact group grants",
			wantContext: "baton-zoom: list members for contact group contact-group-1:",
			run: func(ctx context.Context, client *zoom.Client) (*resource.SyncOpResults, error) {
				group := v2.Resource_builder{
					Id: v2.ResourceId_builder{ResourceType: resourceTypeContactGroup.Id, Resource: "contact-group-1"}.Build(),
				}.Build()
				_, results, err := contactGroupBuilder(client, nil).Grants(ctx, group, resource.SyncOpAttrs{})
				return results, err
			},
		},
		{
			name:        "invite list",
			wantContext: "baton-zoom: list pending invitations:",
			run: func(ctx context.Context, client *zoom.Client) (*resource.SyncOpResults, error) {
				_, results, err := inviteBuilder(client).List(ctx, nil, resource.SyncOpAttrs{})
				return results, err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newRateLimitedZoomServer(t)
			t.Cleanup(srv.Close)

			results, err := tt.run(t.Context(), newZoomTestClient(t, srv.Client(), srv.URL))
			require.Error(t, err)
			assert.Equal(t, codes.Unavailable, status.Code(err))
			assert.Contains(t, err.Error(), tt.wantContext)
			require.NotNil(t, results)
			assert.NotNil(t, results.Annotations)
		})
	}
}

func TestCreateAccountClientErrorIncludesOperationContext(t *testing.T) {
	srv := newRateLimitedZoomServer(t)
	t.Cleanup(srv.Close)
	profile, err := structpb.NewStruct(map[string]any{
		emailKey:       "pending@example.com",
		firstNameKey:   "Pending",
		lastNameKey:    "User",
		displayNameKey: "Pending User",
	})
	require.NoError(t, err)

	_, _, _, err = (&userResourceType{
		client: newZoomTestClient(t, srv.Client(), srv.URL),
	}).CreateAccount(t.Context(), &v2.AccountInfo{Profile: profile}, nil)
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.Contains(t, err.Error(), "baton-zoom: create account:")
}

func newRateLimitedZoomServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		_, err := w.Write([]byte(`{"code":429,"message":"rate limited"}`))
		require.NoError(t, err)
	}))
}
