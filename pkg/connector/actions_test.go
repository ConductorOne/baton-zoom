package connector

import (
	"context"
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

func newZoomTestClient(t *testing.T, httpClient *http.Client, baseURL string) *zoom.Client {
	t.Helper()
	client, err := zoom.NewClient(t.Context(), httpClient, "test-token", baseURL)
	require.NoError(t, err)
	return client
}

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
			// Verify the options sent to Zoom, not only the response message.
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
