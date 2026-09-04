package zoom

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/conductorone/baton-sdk/pkg/uhttp"
)

const (
	defaultBaseURL   = "https://api.zoom.us/v2"
	defaultAuthURL   = "https://zoom.us/oauth/token"
	resourcePageSize = "50"

	// UserNotFoundErrorCode is Zoom's API error code for a missing user.
	UserNotFoundErrorCode = 1001
	// GroupMemberNotFoundErrorCode is Zoom's API error code for a missing group member.
	GroupMemberNotFoundErrorCode = 4131
	// GroupAdminNotFoundErrorCode is Zoom's API error code when the user is
	// not an administrator of the group. DELETE /groups/{groupId}/admins/{userId}
	// returns HTTP 400 with this code, not 404.
	GroupAdminNotFoundErrorCode = 4138
)

// APIError is Zoom's error envelope, e.g. {"code":1001,"message":"User not
// exist: abc"}. Callers inspect Code to tell an explicit "user not found" from
// a generic 404, which is what makes deletion safely idempotent.
//
// It satisfies uhttp.ErrorResponse. The custom error option below returns this
// value in the error chain; uhttp.WithErrorResponse decodes it but returns only
// a gRPC status, which would prevent errors.As from finding the Zoom code.
type APIError struct {
	StatusCode int    `json:"-"`
	Body       string `json:"-"`
	Code       int    `json:"code"`
	Msg        string `json:"message"`
}

var _ uhttp.ErrorResponse = (*APIError)(nil)

func (e *APIError) Error() string {
	return fmt.Sprintf("request failed with status code %d: %s", e.StatusCode, e.Body)
}

func (e *APIError) Message() string {
	if e.Msg != "" {
		return e.Msg
	}
	return e.Body
}

// withZoomErrorResponse decodes Zoom's error envelope while preserving the
// typed error for connector-side idempotency checks.
func withZoomErrorResponse(apiErr *APIError) uhttp.DoOption {
	return func(resp *uhttp.WrapperResponse) error {
		if resp.StatusCode < http.StatusBadRequest {
			return nil
		}

		apiErr.StatusCode = resp.StatusCode
		apiErr.Body = string(resp.Body)
		if err := json.Unmarshal(resp.Body, apiErr); err != nil {
			// A proxy or gateway may return HTML or an empty body. Leave Code
			// unset because only an explicit Zoom code can drive idempotency.
			apiErr.Code = 0
			apiErr.Msg = ""
		}

		return apiErr
	}
}

// withZoomJSONResponse decodes successful responses and accepts empty 2xx
// bodies, including Zoom's common 204 response to mutations.
func withZoomJSONResponse(target any) uhttp.DoOption {
	return func(resp *uhttp.WrapperResponse) error {
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices || len(resp.Body) == 0 {
			return nil
		}
		return uhttp.WithAlwaysJSONResponse(target)(resp)
	}
}

// IsUserNotFound reports whether Zoom explicitly identified a missing user.
// A generic 404 is not enough to establish idempotent deletion.
func IsUserNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) &&
		apiErr.StatusCode == http.StatusNotFound &&
		apiErr.Code == UserNotFoundErrorCode
}

// IsGroupMemberNotFound reports whether Zoom explicitly identified a missing
// group member. A generic 404 is not enough to establish idempotent revocation.
func IsGroupMemberNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) &&
		apiErr.StatusCode == http.StatusNotFound &&
		apiErr.Code == GroupMemberNotFoundErrorCode
}

// IsGroupAdminNotFound reports whether Zoom explicitly identified that the
// user is not a group administrator. A generic 400 is not enough: paid-account
// denials use the same HTTP status with a different Zoom code.
func IsGroupAdminNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) &&
		apiErr.StatusCode == http.StatusBadRequest &&
		apiErr.Code == GroupAdminNotFoundErrorCode
}
