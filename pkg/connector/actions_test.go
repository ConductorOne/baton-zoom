package connector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
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
			name: "wrong resource_type for user_id",
			args: map[string]any{
				argUserID:       map[string]any{"resource_type": "group", "resource": "abc"},
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
			name: "transfer_email without a transfer option",
			args: map[string]any{
				argUserID:        userIDArg("abc"),
				argDeleteAction:  "delete",
				argTransferEmail: "manager@example.com",
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
			// Keep the handler alive after reporting failures.
			t.Errorf("unexpected method %s", r.Method)
			status, body = http.StatusMethodNotAllowed, ""
		}
		w.Header().Set("Content-Type", "application/json")
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
			// Keep the handler alive after reporting failures.
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
	assert.Contains(t, err.Error(), "transfer recipient was not found")
	assert.NotContains(t, err.Error(), "ghost@example.com")
}

func TestTransferAndDeleteUserAction_PreservesMappedAPIError(t *testing.T) {
	srv := mockZoomServer(t,
		func(id string) (int, string) {
			return http.StatusTooManyRequests, `{"code":429,"message":"Too many requests."}`
		},
		func(id string, query map[string][]string) (int, string) {
			t.Error("DELETE should not be called when transfer recipient verification is rate limited")
			return http.StatusInternalServerError, ""
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

	result, _, err := u.transferAndDeleteUserAction(t.Context(), args)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.Contains(t, err.Error(), "verify transfer recipient")
	assert.NotContains(t, err.Error(), "manager@example.com")
}

// A missing user is idempotent only when no transfer was requested.
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

// A 404 cannot prove a requested transfer completed.
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
	assert.Contains(t, err.Error(), "requested transfer cannot be confirmed")
	assert.NotContains(t, err.Error(), "manager@example.com")
}
