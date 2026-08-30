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

func TestDeleteUserWithTransfer_EscapesDotSegments(t *testing.T) {
	const trickyID = "../accounts/me"
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := NewClient(srv.Client(), "test-token", srv.URL)
	err := client.DeleteUserWithTransfer(context.Background(), trickyID, DeleteUserOptions{Action: Delete})
	require.NoError(t, err)

	// Same hazard as GetUser: url.JoinPath would resolve "../" and send this
	// delete to a different Zoom endpoint (e.g. the sub-account disassociate
	// route) than /users/{userId}.
	assert.Equal(t, "/users/"+trickyID, gotPath)
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

func TestGetUser_EscapesSpecialCharacters(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{
			name: "question mark is escaped, not treated as a query separator",
			id:   "user@example.com?admin=true",
		},
		{
			name: "dot-segments are escaped, not resolved to a different endpoint",
			id:   "../accounts/me",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath, gotRawQuery string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotRawQuery = r.URL.RawQuery
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":"resolved"}`))
			}))
			defer srv.Close()

			client := NewClient(srv.Client(), "test-token", srv.URL)
			_, resp, err := client.GetUser(context.Background(), tt.id)
			require.NoError(t, err)
			_ = resp.Body.Close()

			// The id must land in the path verbatim as a single segment, with
			// no query string — url.JoinPath would resolve "../" segments
			// (redirecting the request to a different Zoom endpoint) and
			// leave "?" to split off a bogus query string; PathEscape avoids
			// both.
			assert.Empty(t, gotRawQuery)
			assert.Equal(t, "/users/"+tt.id, gotPath)
		})
	}
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
	assert.Contains(t, apiErr.Body, "User not exist")
}
