package zoom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newTestClient(t *testing.T, httpClient *http.Client, baseURL string) *Client {
	t.Helper()
	client, err := NewClient(t.Context(), httpClient, "test-token", baseURL)
	require.NoError(t, err)
	return client
}

func TestNewClient_TrimsTrailingSlashFromBaseURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resolved"}`))
	}))
	defer srv.Close()

	// A trailing slash on --base-url is a plausible operator mistake (or a
	// deliberate mock-server URL). URL construction must still yield one
	// separator before the resource path.
	client := newTestClient(t, srv.Client(), srv.URL+"/")
	_, resp, err := client.GetUser(context.Background(), "abc123")
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, "/users/abc123", gotPath)
}

func TestNewClient_RejectsInvalidBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
	}{
		{name: "missing scheme", baseURL: "api.zoom.us/v2"},
		{name: "missing host", baseURL: "https:///v2"},
		{name: "invalid escape", baseURL: "https://api.zoom.us/%zz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(t.Context(), http.DefaultClient, "test-token", tt.baseURL)
			require.Error(t, err)
			assert.Nil(t, client)
		})
	}
}

func TestDoRequest_DisablesGETCache(t *testing.T) {
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"user123"}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.Client(), srv.URL)
	for range 2 {
		_, resp, err := client.GetUser(t.Context(), "user123")
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	assert.Equal(t, 2, requestCount, "provisioning reads must observe upstream mutations")
}

func TestDeleteUserWithTransfer_QueryParams(t *testing.T) {
	tests := []struct {
		name      string
		opts      DeleteUserOptions
		wantQuery url.Values
	}{
		{
			name:      "zero options sends no query params",
			opts:      DeleteUserOptions{},
			wantQuery: url.Values{},
		},
		{
			name: "fully populated options",
			opts: DeleteUserOptions{
				Action:            Delete,
				TransferEmail:     "manager@example.com",
				TransferMeeting:   true,
				TransferWebinar:   true,
				TransferRecording: true,
			},
			wantQuery: url.Values{
				"action":             []string{"delete"},
				"transfer_email":     []string{"manager@example.com"},
				"transfer_meeting":   []string{"true"},
				"transfer_webinar":   []string{"true"},
				"transfer_recording": []string{"true"},
			},
		},
		{
			name: "disassociate with no transfer",
			opts: DeleteUserOptions{Action: Disassociate},
			wantQuery: url.Values{
				"action": []string{"disassociate"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotQuery url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.Query()
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()

			client := newTestClient(t, srv.Client(), srv.URL)
			err := client.DeleteUserWithTransfer(context.Background(), "user123", tt.opts)
			require.NoError(t, err)
			assert.Equal(t, tt.wantQuery, gotQuery)
		})
	}
}

func TestDeleteUserWithTransfer_EscapesDotSegments(t *testing.T) {
	const trickyID = "../accounts/me"
	var gotPath, gotEscapedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotEscapedPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.Client(), srv.URL)
	err := client.DeleteUserWithTransfer(context.Background(), trickyID, DeleteUserOptions{Action: Delete})
	require.NoError(t, err)

	// The ID must be escaped before it is passed to url.JoinPath; otherwise
	// JoinPath would resolve "../" and send the delete to a different Zoom
	// endpoint. EscapedPath is the representation that went over the wire.
	assert.Equal(t, "/users/"+trickyID, gotPath)
	assert.Equal(t, "/users/..%2Faccounts%2Fme", gotEscapedPath)
}

func TestDeleteUser_DefaultsToNoQueryParams(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.Client(), srv.URL)
	err := client.DeleteUser(context.Background(), "user123")
	require.NoError(t, err)
	assert.Empty(t, gotQuery)
}

func TestGetUser_EscapesSpecialCharacters(t *testing.T) {
	tests := []struct {
		name            string
		id              string
		wantEscapedPath string
	}{
		{
			name:            "question mark is escaped, not treated as a query separator",
			id:              "user@example.com?admin=true",
			wantEscapedPath: "/users/user@example.com%3Fadmin=true",
		},
		{
			name:            "dot-segments are escaped, not resolved to a different endpoint",
			id:              "../accounts/me",
			wantEscapedPath: "/users/..%2Faccounts%2Fme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath, gotRawQuery, gotEscapedPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotRawQuery = r.URL.RawQuery
				gotEscapedPath = r.URL.EscapedPath()
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":"resolved"}`))
			}))
			defer srv.Close()

			client := newTestClient(t, srv.Client(), srv.URL)
			_, resp, err := client.GetUser(context.Background(), tt.id)
			require.NoError(t, err)
			_ = resp.Body.Close()

			// r.URL.Path is decoded, so it reads identically whether the
			// dot-segment was sent escaped or raw — it can't tell a correct
			// implementation from a broken one. r.URL.EscapedPath() is what
			// actually went out on the wire: an intermediary must see "..%2F",
			// not a real path separator it could normalize away.
			assert.Empty(t, gotRawQuery)
			assert.Equal(t, "/users/"+tt.id, gotPath)
			assert.Equal(t, tt.wantEscapedPath, gotEscapedPath)
		})
	}
}

func TestPatchUserLicense_EscapesDotSegments(t *testing.T) {
	const trickyID = "../accounts/me"
	var gotEscapedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		gotEscapedPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.Client(), srv.URL)
	err := client.PatchUserLicense(context.Background(), trickyID, LicensedUser)
	require.NoError(t, err)
	assert.Equal(t, "/users/..%2Faccounts%2Fme", gotEscapedPath)
}

func TestDoRequest_ErrorIsTypedAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":1001,"message":"User not exist: user123"}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.Client(), srv.URL)
	err := client.DeleteUser(context.Background(), "user123")
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.Equal(t, UserNotFoundErrorCode, apiErr.Code)
	assert.Contains(t, apiErr.Body, "User not exist")
	assert.Equal(t, "User not exist: user123", apiErr.Message())
}

// A 429 must carry the rate-limit description in the status details, or the
// SDK retryer has nothing to schedule backoff from.
func TestDoRequest_RateLimitedErrorCarriesRateLimitDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "10")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("Retry-After", "42")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":429,"message":"Too many requests"}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.Client(), srv.URL)
	err := client.DeleteUser(context.Background(), "user123")
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))

	st, ok := status.FromError(err)
	require.True(t, ok)
	var description *v2.RateLimitDescription
	for _, detail := range st.Details() {
		if rl, isRateLimit := detail.(*v2.RateLimitDescription); isRateLimit {
			description = rl
		}
	}
	require.NotNil(t, description, "429 error is missing the rate limit description")
	assert.Equal(t, v2.RateLimitDescription_STATUS_OVERLIMIT, description.GetStatus())
	assert.Equal(t, int64(10), description.GetLimit())
}

// A non-JSON error body must not leave a decoded Zoom code behind, otherwise a
// proxy 404 could be mistaken for an explicit "user not found".
func TestDoRequest_NonJSONErrorBodyHasNoZoomCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<html>404 Not Found</html>"))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.Client(), srv.URL)
	err := client.DeleteUser(context.Background(), "user123")
	require.Error(t, err)

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Zero(t, apiErr.Code)
	assert.False(t, IsUserNotFound(err))
}
