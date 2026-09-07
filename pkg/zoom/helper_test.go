package zoom

import (
	"errors"
	"net/http"
	"testing"

	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAPIErrorMessageFallsBackToBody(t *testing.T) {
	withMessage := &APIError{Msg: "User not exist: abc", Body: `{"code":1001,"message":"User not exist: abc"}`}
	assert.Equal(t, "User not exist: abc", withMessage.Message())

	// A non-JSON error body has no message field, so the raw body is all the
	// detail there is to report.
	withoutMessage := &APIError{Body: "<html>502 Bad Gateway</html>"}
	assert.Equal(t, "<html>502 Bad Gateway</html>", withoutMessage.Message())
}

func TestMapAuthenticationError(t *testing.T) {
	for _, code := range []string{"invalid_client", "unauthorized_client", "invalid_grant", "invalid_request"} {
		t.Run(code, func(t *testing.T) {
			oauthErr := &OAuthError{
				StatusCode: http.StatusBadRequest,
				Code:       code,
				Reason:     "authentication failed",
			}
			mapped := mapAuthenticationError(oauthErr)
			assert.Equal(t, codes.Unauthenticated, status.Code(mapped))

			var typedErr *OAuthError
			require.True(t, errors.As(mapped, &typedErr))
			assert.Same(t, oauthErr, typedErr)
		})
	}

	genericBadRequest := status.Error(codes.InvalidArgument, "bad request")
	assert.Same(t, genericBadRequest, mapAuthenticationError(genericBadRequest))

	unsupportedGrant := errors.Join(
		genericBadRequest,
		&OAuthError{StatusCode: http.StatusBadRequest, Code: "unsupported_grant_type"},
	)
	assert.Same(t, unsupportedGrant, mapAuthenticationError(unsupportedGrant))
	assert.Equal(t, codes.InvalidArgument, status.Code(unsupportedGrant))
}

func TestWithZoomErrorResponsePreservesDecodedCode(t *testing.T) {
	apiErr := &APIError{}
	err := withZoomErrorResponse(apiErr)(&uhttp.WrapperResponse{
		StatusCode: http.StatusNotFound,
		Body:       []byte(`{"code":1001,"message":124}`),
	})
	require.Error(t, err)
	assert.Equal(t, UserNotFoundErrorCode, apiErr.Code)
	assert.True(t, IsUserNotFound(err))
}

func TestWithZoomOAuthErrorResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantError  bool
		wantCode   string
		wantReason string
	}{
		{
			name:       "success response",
			statusCode: http.StatusOK,
		},
		{
			name:       "valid OAuth error",
			statusCode: http.StatusBadRequest,
			body:       `{"error":"invalid_client","reason":"Invalid client credentials"}`,
			wantError:  true,
			wantCode:   "invalid_client",
			wantReason: "Invalid client credentials",
		},
		{
			name:       "valid JSON preserves decoded code after type error",
			statusCode: http.StatusBadRequest,
			body:       `{"error":"invalid_client","reason":123}`,
			wantError:  true,
			wantCode:   "invalid_client",
		},
		{
			name:       "non-JSON body",
			statusCode: http.StatusBadGateway,
			body:       "<html>Bad Gateway</html>",
			wantError:  true,
		},
		{
			name:       "empty error body",
			statusCode: http.StatusBadGateway,
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oauthErr := &OAuthError{}
			err := withZoomOAuthErrorResponse(oauthErr)(&uhttp.WrapperResponse{
				StatusCode: tt.statusCode,
				Body:       []byte(tt.body),
			})

			if !tt.wantError {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			assert.Same(t, oauthErr, err)
			assert.Equal(t, tt.wantCode, oauthErr.Code)
			assert.Equal(t, tt.wantReason, oauthErr.Reason)
			assert.NotEmpty(t, oauthErr.Error())
		})
	}
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
