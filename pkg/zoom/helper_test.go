package zoom

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAPIErrorMessageFallsBackToBody(t *testing.T) {
	withMessage := &APIError{Msg: "User not exist: abc", Body: `{"code":1001,"message":"User not exist: abc"}`}
	assert.Equal(t, "User not exist: abc", withMessage.Message())

	// A non-JSON error body has no message field, so the raw body is all the
	// detail there is to report.
	withoutMessage := &APIError{Body: "<html>502 Bad Gateway</html>"}
	assert.Equal(t, "<html>502 Bad Gateway</html>", withoutMessage.Message())
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
