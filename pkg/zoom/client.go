package zoom

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"google.golang.org/grpc/codes"
)

// NewClient validates baseURL and wraps httpClient with Baton's HTTP client.
func NewClient(ctx context.Context, httpClient *http.Client, token string, baseURL string) (*Client, error) {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	validatedBaseURL, err := validateBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	baseHTTPClient, err := uhttp.NewBaseHttpClientWithContext(ctx, httpClient)
	if err != nil {
		return nil, fmt.Errorf("create HTTP client: %w", err)
	}
	return &Client{
		httpClient: baseHTTPClient,
		token:      token,
		baseURL:    validatedBaseURL,
	}, nil
}

// RequestAccessToken obtains an account-credentials token from authURL,
// defaulting to https://zoom.us/oauth/token. No API scope is required.
func RequestAccessToken(ctx context.Context, accountId string, clientId string, clientSecret string, authURL string) (string, error) {
	if authURL == "" {
		authURL = defaultAuthURL
	}
	httpClient, err := uhttp.NewBasicAuth(clientId, clientSecret).GetClient(
		ctx,
		uhttp.WithLogger(true, ctxzap.Extract(ctx)),
	)
	if err != nil {
		return "", fmt.Errorf("create authentication client: %w", err)
	}
	baseHTTPClient, err := uhttp.NewBaseHttpClientWithContext(ctx, httpClient)
	if err != nil {
		return "", fmt.Errorf("create authentication HTTP client: %w", err)
	}
	requestURL, err := url.Parse(authURL)
	if err != nil {
		return "", fmt.Errorf("parse authentication URL: %w", err)
	}
	requestURL.RawQuery = url.Values{
		accountIDQueryKey: {accountId},
		grantTypeQueryKey: {accountCredentialsGrant},
	}.Encode()
	req, err := baseHTTPClient.NewRequest(ctx, http.MethodPost, requestURL, uhttp.WithAcceptJSONHeader())
	if err != nil {
		return "", fmt.Errorf("create authentication request: %w", err)
	}
	res := &accessTokenResponse{}
	oauthErr := &OAuthError{}
	resp, err := baseHTTPClient.Do(req, withZoomOAuthErrorResponse(oauthErr), withZoomJSONResponse(res))
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		return "", mapAuthenticationError(err)
	}
	if res.AccessToken == "" {
		return "", fmt.Errorf("authentication response missing access_token")
	}
	return res.AccessToken, nil
}

// GetUsers returns one page of users filtered by status from GET /v2/users.
// Required scope: user:read:list_users:admin.
func (c *Client) GetUsers(ctx context.Context, nextToken string, status string) ([]*User, string, annotations.Annotations, error) {
	endpoint, err := buildEndpoint(c.baseURL, usersPath)
	if err != nil {
		return nil, "", nil, err
	}
	query := paginationQuery(nextToken)
	query.Set(statusQueryKey, status)
	res := &usersResponse{}
	annos, err := c.doRequest(ctx, endpoint, res, http.MethodGet, query, nil)
	if err != nil {
		return nil, "", annos, err
	}
	return res.Users, res.NextPageToken, annos, nil
}

// GetGroups returns one page from GET /v2/groups.
// Required scope: group:read:list_groups:admin.
func (c *Client) GetGroups(ctx context.Context, nextToken string) ([]*Group, string, annotations.Annotations, error) {
	endpoint, err := buildEndpoint(c.baseURL, groupsPath)
	if err != nil {
		return nil, "", nil, err
	}
	res := &groupsResponse{}
	annos, err := c.doRequest(ctx, endpoint, res, http.MethodGet, paginationQuery(nextToken), nil)
	if err != nil {
		return nil, "", annos, err
	}
	return res.Groups, res.NextPageToken, annos, nil
}

// GetContactGroups returns one page from GET /v2/contacts/groups.
// Required scope: contact_group:read:list_groups:admin.
func (c *Client) GetContactGroups(ctx context.Context, nextToken string) ([]*ContactGroup, string, annotations.Annotations, error) {
	endpoint, err := buildEndpoint(c.baseURL, contactsPath, groupsPath)
	if err != nil {
		return nil, "", nil, err
	}
	res := &contactGroupsResponse{}
	annos, err := c.doRequest(ctx, endpoint, res, http.MethodGet, paginationQuery(nextToken), nil)
	if err != nil {
		return nil, "", annos, err
	}
	return res.Groups, res.NextPageToken, annos, nil
}

// GetRoles returns all roles from GET /v2/roles, which is not paginated.
// Required scope: role:read:list_roles:admin.
func (c *Client) GetRoles(ctx context.Context) ([]*Role, annotations.Annotations, error) {
	endpoint, err := buildEndpoint(c.baseURL, rolesPath)
	if err != nil {
		return nil, nil, err
	}
	res := &rolesResponse{}
	annos, err := c.doRequest(ctx, endpoint, res, http.MethodGet, nil, nil)
	if err != nil {
		return nil, annos, err
	}
	return res.Roles, annos, nil
}

// GetGroupAdmins returns one page from GET /v2/groups/{groupId}/admins.
// Required scope: group:read:administrator:admin.
func (c *Client) GetGroupAdmins(ctx context.Context, groupId string, nextToken string) ([]*User, string, annotations.Annotations, error) {
	endpoint, err := buildEndpoint(c.baseURL, groupsPath, groupId, adminsPath)
	if err != nil {
		return nil, "", nil, err
	}
	res := &adminsResponse{}
	annos, err := c.doRequest(ctx, endpoint, res, http.MethodGet, paginationQuery(nextToken), nil)
	if err != nil {
		return nil, "", annos, err
	}
	return res.Admins, res.NextPageToken, annos, nil
}

// GetContactGroupMembers returns one page from GET /v2/contacts/groups/{groupId}/members.
// Required scope: contact_group:read:list_members:admin.
func (c *Client) GetContactGroupMembers(ctx context.Context, groupId string, nextToken string) ([]*GroupMember, string, annotations.Annotations, error) {
	endpoint, err := buildEndpoint(c.baseURL, contactsPath, groupsPath, groupId, membersPath)
	if err != nil {
		return nil, "", nil, err
	}
	res := &contactGroupMembersResponse{}
	annos, err := c.doRequest(ctx, endpoint, res, http.MethodGet, paginationQuery(nextToken), nil)
	if err != nil {
		return nil, "", annos, err
	}
	return res.Members, res.NextPageToken, annos, nil
}

// GetUser returns one user from GET /v2/users/{userId}.
// Required scope: user:read:user:admin.
func (c *Client) GetUser(ctx context.Context, userId string) (*User, annotations.Annotations, error) {
	endpoint, err := buildEndpoint(c.baseURL, usersPath, userId)
	if err != nil {
		return nil, nil, err
	}
	res := &User{}
	annos, err := c.doRequest(ctx, endpoint, res, http.MethodGet, nil, nil)
	if err != nil {
		return nil, annos, err
	}
	return res, annos, nil
}

// EnsureGroupMember POSTs the user into the group and returns whether Zoom
// newly created the membership. Zoom answers 201 even when it added nobody:
// empty or unexpected ids can mean the user already belonged, or that Zoom
// accepted the request and ignored it. When the posted user is not echoed,
// the user's group_ids tells those cases apart. Required scopes:
// group:write:member:admin and, on the ambiguous path, user:read:user:admin.
func (c *Client) EnsureGroupMember(ctx context.Context, groupId, userId string) (bool, annotations.Annotations, error) {
	output := annotations.New()

	endpoint, err := buildEndpoint(c.baseURL, groupsPath, groupId, membersPath)
	if err != nil {
		return false, output, err
	}
	added := &membershipMutationResponse{}
	annos, err := c.doRequest(ctx, endpoint, added, http.MethodPost, nil, idsBody(membersBodyKey, userId))
	output.Merge(annos...)
	if err != nil {
		return false, output, err
	}
	if containsCSVToken(added.IDs, userId) {
		return true, output, nil
	}

	user, annos, err := c.GetUser(ctx, userId)
	output.Merge(annos...)
	if err != nil {
		return false, output, err
	}
	if slices.Contains(user.GroupIDs, groupId) {
		return false, output, nil
	}
	return false, output, uhttp.WrapErrors(codes.FailedPrecondition, "zoom did not add the user to the group")
}

// EnsureGroupAdmin POSTs the user as a group administrator and returns whether
// Zoom newly created the assignment. Confirmation uses the canonical user ID
// when present and falls back to the documented admin email when the payload
// omits the ID. Required scopes:
// group:write:administrator:admin and, on the ambiguous path, group:read:administrator:admin.
func (c *Client) EnsureGroupAdmin(ctx context.Context, groupId, userId, email string) (bool, annotations.Annotations, error) {
	output := annotations.New()
	endpoint, err := buildEndpoint(c.baseURL, groupsPath, groupId, adminsPath)
	if err != nil {
		return false, output, err
	}
	added := &membershipMutationResponse{}
	annos, err := c.doRequest(ctx, endpoint, added, http.MethodPost, nil, idsBody(adminsBodyKey, userId))
	output.Merge(annos...)
	if err != nil {
		return false, output, err
	}
	if containsCSVToken(added.IDs, userId) {
		return true, output, nil
	}

	var token string
	sawEmptyID := false
	for {
		admins, nextToken, annos, err := c.GetGroupAdmins(ctx, groupId, token)
		output.Merge(annos...)
		if err != nil {
			return false, output, err
		}
		for _, admin := range admins {
			if admin.ID == userId {
				return false, output, nil
			}
			if admin.ID == "" {
				if email != "" && strings.EqualFold(admin.Email, email) {
					return false, output, nil
				}
				sawEmptyID = true
			}
		}
		if nextToken == "" {
			if sawEmptyID && email == "" {
				return false, output, uhttp.WrapErrors(codes.InvalidArgument, "group admin assignment requires the user's email")
			}
			return false, output, uhttp.WrapErrors(codes.FailedPrecondition, "zoom did not add the administrator to the group")
		}
		token = nextToken
	}
}

// DeleteGroupAdmin removes an administrator via DELETE /v2/groups/{groupId}/admins/{userId}.
// Required scope: group:delete:administrator:admin.
func (c *Client) DeleteGroupAdmin(ctx context.Context, groupId, userId string) error {
	endpoint, err := buildEndpoint(c.baseURL, groupsPath, groupId, adminsPath, userId)
	if err != nil {
		return err
	}
	_, err = c.doRequest(ctx, endpoint, nil, http.MethodDelete, nil, nil)
	return err
}

// DeleteGroupMember removes a member via DELETE /v2/groups/{groupId}/members/{userId}.
// Required scope: group:delete:member:admin.
func (c *Client) DeleteGroupMember(ctx context.Context, groupId, userId string) error {
	endpoint, err := buildEndpoint(c.baseURL, groupsPath, groupId, membersPath, userId)
	if err != nil {
		return err
	}
	_, err = c.doRequest(ctx, endpoint, nil, http.MethodDelete, nil, nil)
	return err
}

// AssignRole assigns a user via POST /v2/roles/{roleId}/members.
// Required scope: role:write:member:admin.
func (c *Client) AssignRole(ctx context.Context, roleId, userId string) error {
	endpoint, err := buildEndpoint(c.baseURL, rolesPath, roleId, membersPath)
	if err != nil {
		return err
	}
	_, err = c.doRequest(ctx, endpoint, nil, http.MethodPost, nil, idsBody(membersBodyKey, userId))
	return err
}

// UnassignRole removes a user via DELETE /v2/roles/{roleId}/members/{userId}.
// Required scope: role:delete:member:admin.
func (c *Client) UnassignRole(ctx context.Context, roleId, userId string) error {
	endpoint, err := buildEndpoint(c.baseURL, rolesPath, roleId, membersPath, userId)
	if err != nil {
		return err
	}
	_, err = c.doRequest(ctx, endpoint, nil, http.MethodDelete, nil, nil)
	return err
}

// CreateUser creates a user via POST /v2/users.
// Required scope: user:write:user:admin.
func (c *Client) CreateUser(ctx context.Context, newUser *UserCreationBody) (*UserCreationResponse, error) {
	endpoint, err := buildEndpoint(c.baseURL, usersPath)
	if err != nil {
		return nil, err
	}
	res := &UserCreationResponse{}
	_, err = c.doRequest(ctx, endpoint, res, http.MethodPost, nil, newUser)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// DeleteUser removes a user via DELETE /v2/users/{userId} and applies the
// optional transfer settings in opts. A zero DeleteUserOptions leaves Zoom's
// default action unchanged. Zoom requires TransferEmail when any transfer
// flag is enabled; callers must validate that combination.
// Required scope: user:delete:user:admin.
func (c *Client) DeleteUser(ctx context.Context, userId string, opts DeleteUserOptions) error {
	endpoint, err := buildEndpoint(c.baseURL, usersPath, userId)
	if err != nil {
		return err
	}
	_, err = c.doRequest(ctx, endpoint, nil, http.MethodDelete, deleteUserQuery(opts), nil)
	return err
}

// PatchUserLicense updates a user's tier via PATCH /v2/users/{userId}.
// Zoom returns 204 No Content on success.
// Required scope: user:update:user:admin.
func (c *Client) PatchUserLicense(ctx context.Context, userId string, licenseType UserType) error {
	endpoint, err := buildEndpoint(c.baseURL, usersPath, userId)
	if err != nil {
		return err
	}
	_, err = c.doRequest(ctx, endpoint, nil, http.MethodPatch, nil, UserPatchBody{Type: licenseType})
	return err
}

// GetAccountPlanUsage returns base-plan purchased and consumed seat counts
// from GET /v2/accounts/me/plans/usage.
// Required scope: billing:read:plan_usage:admin.
func (c *Client) GetAccountPlanUsage(ctx context.Context) (*PlanUsage, annotations.Annotations, error) {
	endpoint, err := buildEndpoint(c.baseURL, accountsPath, mePath, plansPath, usagePath)
	if err != nil {
		return nil, nil, err
	}
	res := &PlanUsage{}
	annos, err := c.doRequest(ctx, endpoint, res, http.MethodGet, nil, nil)
	if err != nil {
		return nil, annos, err
	}
	return res, annos, nil
}

// doRequest sends one authenticated Zoom API request through uhttp and returns
// the response's rate-limit annotations. Rate-limit data is annotated even on
// failure so callers keep the retry hints Zoom sends with 429 and 5xx.
func (c *Client) doRequest(ctx context.Context, rawURL string, res any, method string, params url.Values, body any) (annotations.Annotations, error) {
	requestURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse request URL: %w", err)
	}
	if params != nil {
		requestURL.RawQuery = params.Encode()
	}

	requestOptions := []uhttp.RequestOption{
		uhttp.WithAcceptJSONHeader(),
		uhttp.WithBearerToken(c.token),
	}
	if method == http.MethodGet {
		requestOptions = append(requestOptions, uhttp.WithNoCache())
	}
	if body != nil {
		requestOptions = append(requestOptions, uhttp.WithJSONBody(body))
	}
	req, err := c.httpClient.NewRequest(ctx, method, requestURL, requestOptions...)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	apiErr := &APIError{}
	rateLimit := &v2.RateLimitDescription{}
	doOptions := []uhttp.DoOption{
		withZoomErrorResponse(apiErr),
		uhttp.WithRatelimitData(rateLimit),
	}
	if res != nil {
		doOptions = append(doOptions, withZoomJSONResponse(res))
	}

	resp, err := c.httpClient.Do(req, doOptions...)
	if resp != nil {
		resp.Body.Close()
	}

	annos := annotations.Annotations{}
	annos.WithRateLimiting(rateLimit)
	return annos, err
}
