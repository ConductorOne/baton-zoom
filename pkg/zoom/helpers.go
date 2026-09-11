package zoom

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"google.golang.org/grpc/codes"
)

const (
	defaultBaseURL   = "https://api.zoom.us/v2"
	defaultAuthURL   = "https://zoom.us/oauth/token"
	resourcePageSize = "50"

	usersPath    = "users"
	groupsPath   = "groups"
	contactsPath = "contacts"
	rolesPath    = "roles"
	membersPath  = "members"
	adminsPath   = "admins"
	accountsPath = "accounts"
	mePath       = "me"
	plansPath    = "plans"
	usagePath    = "usage"

	accountIDQueryKey         = "account_id"
	grantTypeQueryKey         = "grant_type"
	pageSizeQueryKey          = "page_size"
	nextPageTokenQueryKey     = "next_page_token"
	statusQueryKey            = "status"
	actionQueryKey            = "action"
	transferEmailQueryKey     = "transfer_email"
	transferMeetingQueryKey   = "transfer_meeting"
	transferWebinarQueryKey   = "transfer_webinar"
	transferRecordingQueryKey = "transfer_recording"

	accountCredentialsGrant = "account_credentials"
	trueQueryValue          = "true"
	membersBodyKey          = "members"
	adminsBodyKey           = "admins"

	oauthInvalidClient = "invalid_client"
	oauthInvalidGrant  = "invalid_grant"

	// UserNotFoundErrorCode is Zoom's API error code for a missing user.
	UserNotFoundErrorCode = 1001
	// UserAlreadyExistsErrorCode is Zoom's API error code when the email is already on the account.
	UserAlreadyExistsErrorCode = 1005
	// GroupMemberNotFoundErrorCode is Zoom's API error code for a missing group member.
	GroupMemberNotFoundErrorCode = 4131
	// GroupAdminNotFoundErrorCode is Zoom's API error code when the user is not a group admin.
	GroupAdminNotFoundErrorCode = 4138
)

var _ uhttp.ErrorResponse = (*APIError)(nil)

// paginationQuery returns Zoom's standard pagination query.
func paginationQuery(nextToken string) url.Values {
	query := url.Values{
		pageSizeQueryKey: {resourcePageSize},
	}
	if nextToken != "" {
		query.Set(nextPageTokenQueryKey, nextToken)
	}
	return query
}

// Error returns the Zoom API failure with its HTTP response metadata.
func (e *APIError) Error() string {
	return fmt.Sprintf("request failed with status code %d: %s", e.StatusCode, e.Body)
}

// Message returns Zoom's structured message or its raw response body.
func (e *APIError) Message() string {
	if e.Msg != "" {
		return e.Msg
	}
	return e.Body
}

// Error returns Zoom's OAuth reason or its raw response metadata.
func (e *OAuthError) Error() string {
	if e.Reason != "" {
		return e.Reason
	}
	return fmt.Sprintf("request failed with status code %d: %s", e.StatusCode, e.Body)
}

// validateBaseURL validates and normalizes a configured Zoom API base URL.
func validateBaseURL(rawURL string) (string, error) {
	rawURL = strings.TrimRight(rawURL, "/")
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("base URL %q is not valid: %w", rawURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("base URL %q must use http or https", rawURL)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("base URL %q is missing a host", rawURL)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("base URL %q must not include a query or fragment", rawURL)
	}
	return rawURL, nil
}

// buildEndpoint joins the base URL with path segments via url.JoinPath.
func buildEndpoint(baseURL string, parts ...string) (string, error) {
	endpoint, err := url.JoinPath(baseURL, parts...)
	if err != nil {
		return "", fmt.Errorf("build request URL: %w", err)
	}
	return endpoint, nil
}

// mapAuthenticationError maps only credential-related OAuth 400 failures.
func mapAuthenticationError(err error) error {
	oauthErr := &OAuthError{}
	if !errors.As(err, &oauthErr) {
		return err
	}
	if oauthErr.StatusCode != http.StatusBadRequest {
		return err
	}
	switch oauthErr.Code {
	case oauthInvalidClient, oauthInvalidGrant:
		return uhttp.WrapErrors(codes.Unauthenticated, "authentication failed", err)
	default:
		return err
	}
}

// withZoomErrorResponse preserves Zoom's typed API error envelope.
func withZoomErrorResponse(apiErr *APIError) uhttp.DoOption {
	return func(resp *uhttp.WrapperResponse) error {
		if resp.StatusCode < http.StatusBadRequest {
			return nil
		}
		apiErr.StatusCode = resp.StatusCode
		apiErr.Body = string(resp.Body)
		if err := json.Unmarshal(resp.Body, apiErr); err != nil {
			apiErr.Code = 0
			apiErr.Msg = ""
		}
		return apiErr
	}
}

// withZoomOAuthErrorResponse preserves Zoom's typed OAuth error envelope.
func withZoomOAuthErrorResponse(oauthErr *OAuthError) uhttp.DoOption {
	return func(resp *uhttp.WrapperResponse) error {
		if resp.StatusCode < http.StatusBadRequest {
			return nil
		}
		oauthErr.StatusCode = resp.StatusCode
		oauthErr.Body = string(resp.Body)
		if err := json.Unmarshal(resp.Body, oauthErr); err != nil {
			oauthErr.Code = ""
			oauthErr.Reason = ""
		}
		return oauthErr
	}
}

// withZoomJSONResponse decodes JSON on 2xx only. Error bodies stay on the
// typed Zoom error option instead of being decoded into the success target.
func withZoomJSONResponse(target any) uhttp.DoOption {
	return func(resp *uhttp.WrapperResponse) error {
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return nil
		}
		return uhttp.WithJSONResponse(target)(resp)
	}
}

// IsAPIError reports whether err contains the specified Zoom status and code.
func IsAPIError(err error, statusCode, code int) bool {
	apiErr := &APIError{}
	return errors.As(err, &apiErr) &&
		apiErr.StatusCode == statusCode &&
		apiErr.Code == code
}

// idsBody builds Zoom's collection mutation payload.
func idsBody(key, id string) map[string]any {
	return map[string]any{
		key: []*Payload{{ID: id}},
	}
}

// containsCSVToken reports whether csv, a comma-delimited Zoom ids field,
// contains the exact token. Substring matches are not enough: "user-1" must
// not match "user-10".
func containsCSVToken(csv, token string) bool {
	if token == "" {
		return false
	}
	for _, part := range strings.Split(csv, ",") {
		if strings.TrimSpace(part) == token {
			return true
		}
	}
	return false
}

// deleteUserQuery builds optional delete and transfer parameters.
func deleteUserQuery(opts DeleteUserOptions) url.Values {
	if opts.Action == "" && opts.TransferEmail == "" && !opts.TransferMeeting && !opts.TransferWebinar && !opts.TransferRecording {
		return nil
	}
	query := url.Values{}
	if opts.Action != "" {
		query.Set(actionQueryKey, string(opts.Action))
	}
	if opts.TransferEmail != "" {
		query.Set(transferEmailQueryKey, opts.TransferEmail)
	}
	setTrueQuery(query, transferMeetingQueryKey, opts.TransferMeeting)
	setTrueQuery(query, transferWebinarQueryKey, opts.TransferWebinar)
	setTrueQuery(query, transferRecordingQueryKey, opts.TransferRecording)
	return query
}

// setTrueQuery sets key only when enabled.
func setTrueQuery(query url.Values, key string, enabled bool) {
	if enabled {
		query.Set(key, trueQueryValue)
	}
}
