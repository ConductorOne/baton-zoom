package connector

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-zoom/pkg/zoom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestLicenseListPlanUsageErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		statusCode    int
		wantCode      codes.Code
		wantResources int
	}{
		{
			name:          "missing optional scope omits seat counts",
			statusCode:    http.StatusForbidden,
			wantCode:      codes.OK,
			wantResources: len(licenseDefinitions),
		},
		{
			name:       "rate limit failure is propagated",
			statusCode: http.StatusTooManyRequests,
			wantCode:   codes.Unavailable,
		},
		{
			name:       "server failure is propagated",
			statusCode: http.StatusServiceUnavailable,
			wantCode:   codes.Unavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/accounts/me/plans/usage", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(`{"code":1234,"message":"test failure"}`))
			}))
			t.Cleanup(server.Close)

			client, err := zoom.NewClient(t.Context(), server.Client(), "token", server.URL)
			require.NoError(t, err)

			resources, results, err := licenseBuilder(client).List(t.Context(), nil, resource.SyncOpAttrs{})
			assert.Equal(t, tt.wantCode, status.Code(err))
			assert.Len(t, resources, tt.wantResources)
			require.NotNil(t, results)
			require.Len(t, results.Annotations, 1)
		})
	}
}
