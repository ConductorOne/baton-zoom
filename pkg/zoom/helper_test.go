package zoom

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAPIErrorMessageFallsBackToBody(t *testing.T) {
	withMessage := &APIError{Msg: "User not exist: abc", Body: `{"code":1001,"message":"User not exist: abc"}`}
	assert.Equal(t, "User not exist: abc", withMessage.Message())

	withReason := &APIError{Reason: "Invalid client_id or client_secret", OAuthError: "invalid_client"}
	assert.Equal(t, "Invalid client_id or client_secret", withReason.Message())

	// A non-JSON error body has no message field, so the raw body is all the
	// detail there is to report.
	withoutMessage := &APIError{Body: "<html>502 Bad Gateway</html>"}
	assert.Equal(t, "<html>502 Bad Gateway</html>", withoutMessage.Message())
}

func TestMapAuthenticationError(t *testing.T) {
	invalidClient := &APIError{
		StatusCode: http.StatusBadRequest,
		OAuthError: "invalid_client",
		Reason:     "Invalid client_id or client_secret",
	}
	mapped := mapAuthenticationError(invalidClient)
	assert.Equal(t, codes.Unauthenticated, status.Code(mapped))

	var apiErr *APIError
	require.True(t, errors.As(mapped, &apiErr))
	assert.Same(t, invalidClient, apiErr)

	genericBadRequest := status.Error(codes.InvalidArgument, "bad request")
	assert.Same(t, genericBadRequest, mapAuthenticationError(genericBadRequest))
}

func TestIsUserNotFound(t *testing.T) {
	assert.True(t, IsUserNotFound(&APIError{
		StatusCode: http.StatusNotFound,
		Code:       UserNotFoundErrorCode,
	}))
	assert.False(t, IsUserNotFound(&APIError{
		StatusCode: http.StatusNotFound,
		Body:       "generic proxy 404",
	}))
	assert.False(t, IsUserNotFound(&APIError{
		StatusCode: http.StatusNotFound,
		Code:       GroupMemberNotFoundErrorCode,
	}))
	assert.False(t, IsUserNotFound(assert.AnError))
}

func TestIsGroupMemberNotFound(t *testing.T) {
	assert.True(t, IsGroupMemberNotFound(&APIError{
		StatusCode: http.StatusNotFound,
		Code:       GroupMemberNotFoundErrorCode,
	}))
	assert.False(t, IsGroupMemberNotFound(&APIError{
		StatusCode: http.StatusNotFound,
		Code:       UserNotFoundErrorCode,
	}))
	assert.False(t, IsGroupMemberNotFound(&APIError{
		StatusCode: http.StatusBadRequest,
		Code:       GroupMemberNotFoundErrorCode,
	}))
	assert.False(t, IsGroupMemberNotFound(assert.AnError))
}

func TestIsGroupAdminNotFound(t *testing.T) {
	assert.True(t, IsGroupAdminNotFound(&APIError{
		StatusCode: http.StatusBadRequest,
		Code:       GroupAdminNotFoundErrorCode,
	}))
	assert.False(t, IsGroupAdminNotFound(&APIError{
		StatusCode: http.StatusBadRequest,
		Code:       200, // Zoom "Only available for Paid account"
	}))
	assert.False(t, IsGroupAdminNotFound(&APIError{
		StatusCode: http.StatusNotFound,
		Code:       GroupAdminNotFoundErrorCode,
	}))
	assert.False(t, IsGroupAdminNotFound(&APIError{
		StatusCode: http.StatusNotFound,
		Code:       GroupMemberNotFoundErrorCode,
	}))
	assert.False(t, IsGroupAdminNotFound(assert.AnError))
}
