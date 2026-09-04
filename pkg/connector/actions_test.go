package connector

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/conductorone/baton-sdk/pkg/actions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/conductorone/baton-zoom/pkg/zoom"
)

func newActionArgs(t *testing.T, fields map[string]any) *structpb.Struct {
	t.Helper()
	s, err := structpb.NewStruct(fields)
	require.NoError(t, err)
	return s
}

func userIDArg(id string) map[string]any {
	return map[string]any{"resource_type": resourceTypeUser.Id, "resource": id}
}

func TestResourceActionsClonesSharedSchema(t *testing.T) {
	ctx := context.Background()
	manager := actions.NewActionManager(ctx)
	registry, err := manager.GetTypeRegistry(ctx, resourceTypeUser.Id)
	require.NoError(t, err)

	original := proto.Clone(transferAndDeleteUserSchema)
	u := &userResourceType{}
	require.NoError(t, u.ResourceActions(ctx, registry))

	assert.True(t, proto.Equal(original, transferAndDeleteUserSchema))
	schemas, _, err := manager.ListActionSchemas(ctx, resourceTypeUser.Id)
	require.NoError(t, err)
	require.Len(t, schemas, 1)
	assert.NotSame(t, transferAndDeleteUserSchema, schemas[0])
	assert.Equal(t, resourceTypeUser.Id, schemas[0].GetResourceTypeId())
}

func TestTransferAndDeleteUserAction_ArgValidation(t *testing.T) {
	u := &userResourceType{}

	tests := []struct {
		name string
		args map[string]any
	}{
		{
			name: "missing user_id",
			args: map[string]any{argDeleteAction: "delete"},
		},
		{
			name: "missing action",
			args: map[string]any{argUserID: userIDArg("abc")},
		},
		{
			name: "empty user_id resource",
			args: map[string]any{
				argUserID:       map[string]any{"resource_type": resourceTypeUser.Id, "resource": ""},
				argDeleteAction: "delete",
			},
		},
		{
			name: "wrong resource_type for user_id",
			args: map[string]any{
				argUserID:       map[string]any{"resource_type": "group", "resource": "abc"},
				argDeleteAction: "delete",
			},
		},
		{
			name: "missing resource_type for user_id",
			args: map[string]any{
				argUserID:       map[string]any{"resource": "abc"},
				argDeleteAction: "delete",
			},
		},
		{
			name: "user_id containing a slash",
			args: map[string]any{
				argUserID:       userIDArg("../accounts/me"),
				argDeleteAction: "delete",
			},
		},
		{
			name: "user_id is a dot segment",
			args: map[string]any{
				argUserID:       userIDArg(".."),
				argDeleteAction: "delete",
			},
		},
		{
			name: "transfer_email containing a slash",
			args: map[string]any{
				argUserID:          userIDArg("abc"),
				argDeleteAction:    "delete",
				argTransferEmail:   "../accounts/me",
				argTransferMeeting: true,
			},
		},
		{
			name: "transfer_email is a dot segment",
			args: map[string]any{
				argUserID:          userIDArg("abc"),
				argDeleteAction:    "delete",
				argTransferEmail:   ".",
				argTransferMeeting: true,
			},
		},
		{
			name: "transfer_email without a transfer option",
			args: map[string]any{
				argUserID:        userIDArg("abc"),
				argDeleteAction:  "delete",
				argTransferEmail: "manager@example.com",
			},
		},
		{
			name: "wrong type for transfer_email",
			args: map[string]any{
				argUserID:          userIDArg("abc"),
				argDeleteAction:    "delete",
				argTransferEmail:   true,
				argTransferMeeting: true,
			},
		},
		{
			name: "wrong type for transfer_meeting",
			args: map[string]any{
				argUserID:          userIDArg("abc"),
				argDeleteAction:    "delete",
				argTransferEmail:   "manager@example.com",
				argTransferMeeting: "true",
			},
		},
		{
			name: "wrong type for transfer_webinar",
			args: map[string]any{
				argUserID:          userIDArg("abc"),
				argDeleteAction:    "delete",
				argTransferEmail:   "manager@example.com",
				argTransferWebinar: "true",
			},
		},
		{
			name: "wrong type for transfer_recording",
			args: map[string]any{
				argUserID:            userIDArg("abc"),
				argDeleteAction:      "delete",
				argTransferEmail:     "manager@example.com",
				argTransferRecording: "true",
			},
		},
		{
			name: "invalid action value",
			args: map[string]any{argUserID: userIDArg("abc"), argDeleteAction: "wipe"},
		},
		{
			name: "transfer_meeting without transfer_email",
			args: map[string]any{
				argUserID:          userIDArg("abc"),
				argDeleteAction:    "delete",
				argTransferMeeting: true,
			},
		},
		{
			name: "transfer_webinar without transfer_email",
			args: map[string]any{
				argUserID:          userIDArg("abc"),
				argDeleteAction:    "delete",
				argTransferWebinar: true,
			},
		},
		{
			name: "transfer_recording without transfer_email",
			args: map[string]any{
				argUserID:            userIDArg("abc"),
				argDeleteAction:      "delete",
				argTransferRecording: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := newActionArgs(t, tt.args)
			result, _, err := u.transferAndDeleteUserAction(context.Background(), args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func TestMapAPIError(t *testing.T) {
	// These expectations follow uhttp.GrpcCodeFromHTTPStatus (baton-sdk
	// v0.25.0) rather than a hand-rolled mapping: 429 and 5xx map to
	// Unavailable specifically because the SDK's retry classifier
	// (pkg/retry) only retries codes.Unavailable and codes.DeadlineExceeded —
	// anything else (e.g. ResourceExhausted, Internal) is treated as a
	// permanent failure even for a transient rate limit or server error.
	tests := []struct {
		name       string
		err        error
		wantCode   codes.Code
		wantStatus bool
	}{
		{name: "401 maps to Unauthenticated", err: &zoom.APIError{StatusCode: http.StatusUnauthorized}, wantCode: codes.Unauthenticated, wantStatus: true},
		{name: "403 maps to PermissionDenied", err: &zoom.APIError{StatusCode: http.StatusForbidden}, wantCode: codes.PermissionDenied, wantStatus: true},
		{name: "404 maps to NotFound", err: &zoom.APIError{StatusCode: http.StatusNotFound}, wantCode: codes.NotFound, wantStatus: true},
		{name: "429 maps to Unavailable (retryable)", err: &zoom.APIError{StatusCode: http.StatusTooManyRequests}, wantCode: codes.Unavailable, wantStatus: true},
		{name: "500 maps to Unavailable (retryable)", err: &zoom.APIError{StatusCode: http.StatusInternalServerError}, wantCode: codes.Unavailable, wantStatus: true},
		{name: "503 maps to Unavailable (retryable)", err: &zoom.APIError{StatusCode: http.StatusServiceUnavailable}, wantCode: codes.Unavailable, wantStatus: true},
		{name: "400 maps to InvalidArgument", err: &zoom.APIError{StatusCode: http.StatusBadRequest}, wantCode: codes.InvalidArgument, wantStatus: true},
		{name: "409 maps to AlreadyExists", err: &zoom.APIError{StatusCode: http.StatusConflict}, wantCode: codes.AlreadyExists, wantStatus: true},
		{name: "already-mapped status is preserved", err: status.Error(codes.Unavailable, "rate limited"), wantCode: codes.Unavailable, wantStatus: true},
		{name: "non-APIError is left unmapped", err: assert.AnError, wantStatus: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapped := mapAPIError(tt.err, "baton-zoom: test")
			if tt.wantStatus {
				assert.Equal(t, tt.wantCode, status.Code(mapped))

				var apiErr *zoom.APIError
				if errors.As(tt.err, &apiErr) {
					assert.True(t, errors.As(mapped, &apiErr), "errors.As should still find the wrapped *zoom.APIError")
				}
			} else {
				assert.Equal(t, codes.Unknown, status.Code(mapped))
			}
		})
	}
}

// mockZoomServer dispatches GET /users/{id} and DELETE /users/{id} to the
// supplied handlers, recording the DELETE call's query string.
func mockZoomServer(t *testing.T, getUser func(id string) (status int, body string), deleteUser func(id string, query map[string][]string) (status int, body string)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/users/"):]
		var status int
		var body string
		switch r.Method {
		case http.MethodGet:
			status, body = getUser(id)
		case http.MethodDelete:
			status, body = deleteUser(id, r.URL.Query())
		default:
			// t.Fatal/Fatalf must run on the test goroutine — calling it here
			// would Goexit the handler without writing a response. t.Errorf
			// is goroutine-safe: it flags the failure without exiting, and
			// the handler still returns a real response below.
			t.Errorf("unexpected method %s", r.Method)
			status, body = http.StatusMethodNotAllowed, ""
		}
		w.WriteHeader(status)
		if body != "" {
			_, _ = w.Write([]byte(body))
		}
	}))
}

func newTestUserResourceType(t *testing.T, baseURL string) *userResourceType {
	t.Helper()
	return &userResourceType{
		resourceType: nil,
		client:       newZoomTestClient(t, http.DefaultClient, baseURL),
	}
}

func TestTransferAndDeleteUserAction_TransferEmailNotFound(t *testing.T) {
	srv := mockZoomServer(t,
		func(id string) (int, string) { return http.StatusNotFound, `{"code":1001,"message":"User not exist"}` },
		func(id string, query map[string][]string) (int, string) {
			// t.Error, not t.Fatal: this runs on the httptest handler
			// goroutine, and Fatal's Goexit would leave the request without
			// a response instead of failing the test cleanly.
			t.Error("DELETE should not be called when transfer_email verification fails")
			return http.StatusInternalServerError, ""
		},
	)
	defer srv.Close()

	u := newTestUserResourceType(t, srv.URL)
	args := newActionArgs(t, map[string]any{
		argUserID:          userIDArg("abc"),
		argDeleteAction:    "delete",
		argTransferEmail:   "ghost@example.com",
		argTransferMeeting: true,
	})

	result, _, err := u.transferAndDeleteUserAction(context.Background(), args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Contains(t, err.Error(), "ghost@example.com")
}

// TestTransferAndDeleteUserAction_AlreadyDeletedIsSuccess deliberately sets
// no transfer flags: the delete-404-is-success shortcut is only safe here
// because nothing was promised to be transferred. See the sibling
// _TransferRequestedAndAlreadyDeletedIsError test for the opposite case.
func TestTransferAndDeleteUserAction_AlreadyDeletedIsSuccess(t *testing.T) {
	srv := mockZoomServer(t,
		func(id string) (int, string) { return http.StatusOK, `{"id":"manager"}` },
		func(id string, query map[string][]string) (int, string) {
			return http.StatusNotFound, `{"code":1001,"message":"User not exist"}`
		},
	)
	defer srv.Close()

	u := newTestUserResourceType(t, srv.URL)
	args := newActionArgs(t, map[string]any{
		argUserID:       userIDArg("abc"),
		argDeleteAction: "delete",
	})

	result, _, err := u.transferAndDeleteUserAction(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Fields["success"].GetBoolValue())
	assert.Contains(t, result.Fields["message"].GetStringValue(), "already removed")
}

func TestTransferAndDeleteUserAction_GenericDelete404IsError(t *testing.T) {
	srv := mockZoomServer(t,
		func(id string) (int, string) { return http.StatusOK, `{"id":"manager"}` },
		func(id string, query map[string][]string) (int, string) {
			return http.StatusNotFound, `upstream route not found`
		},
	)
	defer srv.Close()

	u := newTestUserResourceType(t, srv.URL)
	args := newActionArgs(t, map[string]any{
		argUserID:       userIDArg("abc"),
		argDeleteAction: "delete",
	})

	result, _, err := u.transferAndDeleteUserAction(context.Background(), args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

// TestTransferAndDeleteUserAction_TransferRequestedAndAlreadyDeletedIsError
// covers the case the previous test can't: when a transfer WAS requested,
// the recipient preflight only proves transfer_email existed before the
// delete call — it's not proof the transfer itself completed. A 404 on
// delete here must not be reported as success, since that would silently
// overclaim "data transferred" when Zoom gives no way to confirm it was.
func TestTransferAndDeleteUserAction_TransferRequestedAndAlreadyDeletedIsError(t *testing.T) {
	srv := mockZoomServer(t,
		func(id string) (int, string) { return http.StatusOK, `{"id":"manager"}` },
		func(id string, query map[string][]string) (int, string) {
			return http.StatusNotFound, `{"code":1001,"message":"User not exist"}`
		},
	)
	defer srv.Close()

	u := newTestUserResourceType(t, srv.URL)
	args := newActionArgs(t, map[string]any{
		argUserID:          userIDArg("abc"),
		argDeleteAction:    "delete",
		argTransferEmail:   "manager@example.com",
		argTransferMeeting: true,
	})

	result, _, err := u.transferAndDeleteUserAction(context.Background(), args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Contains(t, err.Error(), "manager@example.com")
}

func TestTransferAndDeleteUserAction_SuccessMessages(t *testing.T) {
	tests := []struct {
		name        string
		args        map[string]any
		wantMessage string
		wantQuery   url.Values
	}{
		{
			name:        "no transfer flags set",
			args:        map[string]any{argUserID: userIDArg("abc"), argDeleteAction: "delete"},
			wantMessage: "user abc deleted from the account",
			wantQuery:   url.Values{"action": []string{"delete"}},
		},
		{
			name: "transfer_meeting set",
			args: map[string]any{
				argUserID:          userIDArg("abc"),
				argDeleteAction:    "disassociate",
				argTransferEmail:   "manager@example.com",
				argTransferMeeting: true,
			},
			wantMessage: "user abc data transferred and disassociated from the account",
			// Asserting the actual query sent to the client — not just the
			// human-readable message — catches a field swapped in the
			// zoom.DeleteUserOptions{} literal (e.g. TransferWebinar for
			// TransferMeeting) that the message text alone wouldn't reveal.
			wantQuery: url.Values{
				"action":           []string{"disassociate"},
				"transfer_email":   []string{"manager@example.com"},
				"transfer_meeting": []string{"true"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotQuery url.Values
			srv := mockZoomServer(t,
				func(id string) (int, string) { return http.StatusOK, `{"id":"manager"}` },
				func(id string, query map[string][]string) (int, string) {
					gotQuery = query
					return http.StatusNoContent, ""
				},
			)
			defer srv.Close()

			u := newTestUserResourceType(t, srv.URL)
			result, _, err := u.transferAndDeleteUserAction(context.Background(), newActionArgs(t, tt.args))
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.True(t, result.Fields["success"].GetBoolValue())
			assert.Equal(t, tt.wantMessage, result.Fields["message"].GetStringValue())
			assert.Equal(t, tt.wantQuery, gotQuery)
		})
	}
}
