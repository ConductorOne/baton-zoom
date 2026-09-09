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

func TestGetUser_TrailingSlashInBaseURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resolved"}`))
	}))
	defer srv.Close()

	// url.JoinPath must normalize a trailing base URL slash.
	client := newTestClient(t, srv.Client(), srv.URL+"/")
	_, _, err := client.GetUser(context.Background(), "abc123")
	require.NoError(t, err)

	assert.Equal(t, "/users/abc123", gotPath)
}

func TestDeleteUser_QueryParams(t *testing.T) {
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
			err := client.DeleteUser(context.Background(), "user123", tt.opts)
			require.NoError(t, err)
			assert.Equal(t, tt.wantQuery, gotQuery)
		})
	}
}

func TestDeleteUser_DefaultsToNoQueryParams(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.Client(), srv.URL)
	err := client.DeleteUser(context.Background(), "user123", DeleteUserOptions{})
	require.NoError(t, err)
	assert.Empty(t, gotQuery)
}

func TestGetUser_EscapesQuerySeparatorInID(t *testing.T) {
	const id = "user@example.com?admin=true"
	var gotPath, gotRawQuery, gotEscapedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRawQuery = r.URL.RawQuery
		gotEscapedPath = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resolved"}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.Client(), srv.URL)
	_, _, err := client.GetUser(context.Background(), id)
	require.NoError(t, err)

	// EscapedPath and RawQuery verify that "?" remains part of the ID.
	assert.Empty(t, gotRawQuery)
	assert.Equal(t, "/users/"+id, gotPath)
	assert.Equal(t, "/users/user@example.com%3Fadmin=true", gotEscapedPath)
}

func TestDoRequest_Generic404IsNotUserNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<html>404 Not Found</html>"))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.Client(), srv.URL)
	err := client.DeleteUser(context.Background(), "user123", DeleteUserOptions{})
	require.Error(t, err)

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Zero(t, apiErr.Code)
	assert.False(t, IsAPIError(err, http.StatusNotFound, UserNotFoundErrorCode))
}

func TestDoRequestAnnotatesRateLimitOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Limit", "100")
		w.Header().Set("X-RateLimit-Remaining", "37")
		_, _ = w.Write([]byte(`{"id":"user123"}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.Client(), srv.URL)
	_, annos, err := client.GetUser(t.Context(), "user123")
	require.NoError(t, err)

	description := &v2.RateLimitDescription{}
	found, err := annos.Pick(description)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, int64(100), description.GetLimit())
	assert.Equal(t, int64(37), description.GetRemaining())
}

// An empty ids field is ambiguous, so the client reads the user's group_ids
// instead of scanning the group: present means the membership already existed,
// absent means Zoom accepted the request and added nobody.
func TestEnsureGroupMemberResolvesAmbiguousCreate(t *testing.T) {
	tests := []struct {
		name        string
		ids         string
		userBody    string
		wantCreated bool
		wantCode    codes.Code
	}{
		{
			name:        "echoed user was created",
			ids:         "user-id",
			wantCreated: true,
		},
		{
			name:        "empty ids and the user already belongs to the group",
			userBody:    `{"id":"user-id","group_ids":["other-group","group-id"]}`,
			wantCreated: false,
		},
		{
			name:     "empty ids and the user belongs to no group",
			userBody: `{"id":"user-id"}`,
			wantCode: codes.FailedPrecondition,
		},
		{
			name:     "empty ids and the user belongs to another group",
			userBody: `{"id":"user-id","group_ids":["other-group"]}`,
			wantCode: codes.FailedPrecondition,
		},
		{
			name:        "unexpected echoed id still confirms existing membership",
			ids:         "someone-else",
			userBody:    `{"id":"user-id","group_ids":["group-id"]}`,
			wantCreated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userReads := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.Method {
				case http.MethodPost:
					assert.Equal(t, "/groups/group-id/members", r.URL.Path)
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"ids":"` + tt.ids + `"}`))
				case http.MethodGet:
					assert.Equal(t, "/users/user-id", r.URL.Path)
					userReads++
					_, _ = w.Write([]byte(tt.userBody))
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
			}))
			defer srv.Close()

			created, _, err := newTestClient(t, srv.Client(), srv.URL).EnsureGroupMember(t.Context(), "group-id", "user-id")
			if tt.wantCode != codes.OK {
				require.Error(t, err)
				assert.Equal(t, tt.wantCode, status.Code(err))
				assert.Equal(t, 1, userReads)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantCreated, created)
			if tt.userBody == "" {
				assert.Zero(t, userReads)
			} else {
				assert.Equal(t, 1, userReads)
			}
		})
	}
}

func TestEnsureGroupAdminConfirmationIdentity(t *testing.T) {
	tests := []struct {
		name     string
		admin    string
		wantCode codes.Code
	}{
		{
			name:     "mismatched non-empty ID does not fall back to matching email",
			admin:    `{"id":"different-user","email":"user-id@example.com","name":"User"}`,
			wantCode: codes.FailedPrecondition,
		},
		{
			name:  "matching ID confirms assignment",
			admin: `{"id":"user-id","email":"different@example.com","name":"User"}`,
		},
		{
			name:  "missing ID falls back to matching email",
			admin: `{"email":"User-ID@example.com","name":"User"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adminReads := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.Method {
				case http.MethodPost:
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"ids":""}`))
				case http.MethodGet:
					adminReads++
					assert.Equal(t, "/groups/group-id/admins", r.URL.Path)
					_, _ = w.Write([]byte(`{"admins":[` + tt.admin + `],"next_page_token":""}`))
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
			}))
			t.Cleanup(srv.Close)

			created, _, err := newTestClient(t, srv.Client(), srv.URL).EnsureGroupAdmin(t.Context(), "group-id", "user-id", "user-id@example.com")
			if tt.wantCode != codes.OK {
				require.Error(t, err)
				assert.Equal(t, tt.wantCode, status.Code(err))
			} else {
				require.NoError(t, err)
				assert.False(t, created)
			}
			assert.Equal(t, 1, adminReads)
		})
	}
}

func TestDoRequestPreservesHTTPClassification(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantCode   codes.Code
	}{
		{name: "generic bad request", statusCode: http.StatusBadRequest, wantCode: codes.InvalidArgument},
		{name: "rate limited", statusCode: http.StatusTooManyRequests, wantCode: codes.Unavailable},
		{name: "server error", statusCode: http.StatusServiceUnavailable, wantCode: codes.Unavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-RateLimit-Limit", "10")
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("Retry-After", "42")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(`{"code":300,"message":"request failed"}`))
			}))
			defer srv.Close()

			client := newTestClient(t, srv.Client(), srv.URL)
			err := client.DeleteUser(t.Context(), "user123", DeleteUserOptions{})
			require.Error(t, err)
			assert.Equal(t, tt.wantCode, status.Code(err))

			if tt.wantCode == codes.Unavailable {
				st := status.Convert(err)
				var description *v2.RateLimitDescription
				for _, detail := range st.Details() {
					if rateLimit, ok := detail.(*v2.RateLimitDescription); ok {
						description = rateLimit
					}
				}
				require.NotNil(t, description)
			}
		})
	}
}

func TestDoRequest_ErrorIsTypedAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":1001,"message":"User not exist: user123"}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.Client(), srv.URL)
	err := client.DeleteUser(context.Background(), "user123", DeleteUserOptions{})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.Equal(t, UserNotFoundErrorCode, apiErr.Code)
	assert.Contains(t, apiErr.Body, "User not exist")
	assert.Equal(t, "User not exist: user123", apiErr.Message())
	assert.True(t, IsAPIError(err, http.StatusNotFound, UserNotFoundErrorCode))
}

func TestDoRequest_ErrorBodyCannotSpoofStatusOrBody(t *testing.T) {
	// Payload fields must not replace the actual HTTP status or body.
	const hostileBody = `{"statusCode":404,"body":"spoofed","code":1001,"message":"User does not exist"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(hostileBody))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.Client(), srv.URL)
	err := client.DeleteUser(context.Background(), "user123", DeleteUserOptions{})
	require.Error(t, err)

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.Equal(t, hostileBody, apiErr.Body)
	assert.False(t, IsAPIError(err, http.StatusNotFound, UserNotFoundErrorCode))

	// The Zoom-owned fields still decode normally.
	assert.Equal(t, UserNotFoundErrorCode, apiErr.Code)
	assert.Equal(t, "User does not exist", apiErr.Msg)
}
