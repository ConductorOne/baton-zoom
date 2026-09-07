package zoom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUser_TrailingSlashInBaseURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resolved"}`))
	}))
	defer srv.Close()

	// A trailing slash on --base-url is a plausible operator mistake, so the
	// path must not come out as ".../v2//users/abc123".
	client := NewClient(srv.Client(), "test-token", srv.URL+"/")
	_, resp, err := client.GetUser(context.Background(), "abc123")
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, "/users/abc123", gotPath)
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

			client := NewClient(srv.Client(), "test-token", srv.URL)
			err := client.DeleteUserWithTransfer(context.Background(), "user123", tt.opts)
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

	client := NewClient(srv.Client(), "test-token", srv.URL)
	err := client.DeleteUser(context.Background(), "user123")
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
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resolved"}`))
	}))
	defer srv.Close()

	client := NewClient(srv.Client(), "test-token", srv.URL)
	_, resp, err := client.GetUser(context.Background(), id)
	require.NoError(t, err)
	_ = resp.Body.Close()

	// r.URL.Path is decoded, so it can't distinguish a "?" that stayed in the
	// path from one that opened a query string. EscapedPath and RawQuery are
	// what actually went out on the wire.
	assert.Empty(t, gotRawQuery)
	assert.Equal(t, "/users/"+id, gotPath)
	assert.Equal(t, "/users/user@example.com%3Fadmin=true", gotEscapedPath)
}

func TestDoRequest_ErrorIsTypedAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":1001,"message":"User not exist: user123"}`))
	}))
	defer srv.Close()

	client := NewClient(srv.Client(), "test-token", srv.URL)
	err := client.DeleteUser(context.Background(), "user123")
	require.Error(t, err)

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.Equal(t, UserNotFoundErrorCode, apiErr.Code)
	assert.Contains(t, apiErr.Body, "User not exist")
	assert.True(t, IsUserNotFound(err))
}

func TestDoRequest_ErrorBodyCannotSpoofStatusOrBody(t *testing.T) {
	// An intermediary (WAF, gateway) can return an envelope whose keys collide
	// with APIError's own fields. Those must come from the real response, or a
	// 502 could be read as Zoom's 404/1001 and a destructive delete would
	// report the user as already removed.
	const hostileBody = `{"statusCode":404,"body":"spoofed","code":1001,"message":"User does not exist"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(hostileBody))
	}))
	defer srv.Close()

	client := NewClient(srv.Client(), "test-token", srv.URL)
	err := client.DeleteUser(context.Background(), "user123")
	require.Error(t, err)

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.Equal(t, hostileBody, apiErr.Body)
	assert.False(t, IsUserNotFound(err))

	// The Zoom-owned fields still decode normally.
	assert.Equal(t, UserNotFoundErrorCode, apiErr.Code)
	assert.Equal(t, "User does not exist", apiErr.Message)
}

func TestDoRequest_Generic404IsNotUserNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<html>404 Not Found</html>"))
	}))
	defer srv.Close()

	client := NewClient(srv.Client(), "test-token", srv.URL)
	err := client.DeleteUser(context.Background(), "user123")
	require.Error(t, err)

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Zero(t, apiErr.Code)
	assert.False(t, IsUserNotFound(err))
}
