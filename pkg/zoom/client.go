package zoom

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"google.golang.org/grpc/codes"
)

type Client struct {
	httpClient *uhttp.BaseHttpClient
	token      string
	baseURL    string
}

// NewClient wraps the provided transport with Baton's HTTP client and
// configures a Zoom API client.
func NewClient(ctx context.Context, httpClient *http.Client, token string, baseURL string) (*Client, error) {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	// Normalize operator-settable base URLs for the client's pre-existing
	// resource URL builders.
	baseURL = strings.TrimRight(baseURL, "/")
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL %q: %w", baseURL, err)
	}
	if parsedBaseURL.Scheme == "" || parsedBaseURL.Host == "" {
		return nil, fmt.Errorf("invalid base URL %q: scheme and host are required", baseURL)
	}

	baseHTTPClient, err := uhttp.NewBaseHttpClientWithContext(ctx, httpClient)
	if err != nil {
		return nil, fmt.Errorf("create HTTP client: %w", err)
	}

	return &Client{
		httpClient: baseHTTPClient,
		token:      token,
		baseURL:    baseURL,
	}, nil
}

type Payload struct {
	ID string `json:"id"`
}

type PaginationData struct {
	NextPageToken string `json:"next_page_token"`
	PageSize      int    `json:"page_size"`
	TotalRecords  int    `json:"total_records"`
}

// returns query params with pagination options.
func paginationQuery(nextToken string) url.Values {
	q := url.Values{}
	q.Add("next_page_token", nextToken)
	q.Add("page_size", resourcePageSize)
	return q
}

// RequestAccessToken creates bearer token needed to use the Zoom API.
func RequestAccessToken(ctx context.Context, accountId string, clientId string, clientSecret string) (string, error) {
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

	requestURL, err := url.Parse(defaultAuthURL)
	if err != nil {
		return "", fmt.Errorf("parse authentication URL: %w", err)
	}
	requestURL.RawQuery = url.Values{
		"account_id": []string{accountId},
		"grant_type": []string{"account_credentials"},
	}.Encode()

	req, err := baseHTTPClient.NewRequest(ctx, http.MethodPost, requestURL, uhttp.WithAcceptJSONHeader())
	if err != nil {
		return "", fmt.Errorf("create authentication request: %w", err)
	}

	var res struct {
		AccessToken string `json:"access_token"`
	}
	apiErr := &APIError{}
	resp, err := baseHTTPClient.Do(req, withZoomErrorResponse(apiErr), withZoomJSONResponse(&res))
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		if apiErr.StatusCode == http.StatusBadRequest || apiErr.StatusCode == http.StatusUnauthorized {
			return "", uhttp.WrapErrors(codes.Unauthenticated, "authentication failed", err)
		}
		return "", err
	}
	if res.AccessToken == "" {
		return "", fmt.Errorf("authentication response missing access_token")
	}
	return res.AccessToken, nil
}

// GetUsers returns Zoom users filtered by status ("active", "inactive", or "pending").
func (c *Client) GetUsers(ctx context.Context, nextToken string, status string) ([]User, string, *http.Response, error) {
	url := fmt.Sprint(c.baseURL, "/users")
	var res struct {
		PaginationData
		Users []User `json:"users"`
	}

	q := paginationQuery(nextToken)
	q.Set("status", status)
	resp, err := c.doRequest(ctx, url, &res, http.MethodGet, q, nil)
	if err != nil {
		return nil, "", nil, err
	}

	if res.NextPageToken != "" {
		return res.Users, res.NextPageToken, resp, nil
	}

	return res.Users, "", resp, nil
}

// GetGroups returns all Zoom groups.
func (c *Client) GetGroups(ctx context.Context, nextToken string) ([]Group, string, *http.Response, error) {
	url := fmt.Sprint(c.baseURL, "/groups")
	var res struct {
		PaginationData
		Groups []Group `json:"groups"`
	}

	q := paginationQuery(nextToken)
	resp, err := c.doRequest(ctx, url, &res, http.MethodGet, q, nil)
	if err != nil {
		return nil, "", nil, err
	}

	if res.NextPageToken != "" {
		return res.Groups, res.NextPageToken, resp, nil
	}

	return res.Groups, "", resp, nil
}

// GetContactGroups returns all contact groups from Zoom.
func (c *Client) GetContactGroups(ctx context.Context, nextToken string) ([]ContactGroup, string, *http.Response, error) {
	url := fmt.Sprint(c.baseURL, "/contacts/groups")
	var res struct {
		PaginationData
		Groups []ContactGroup `json:"groups"`
	}

	q := paginationQuery(nextToken)
	resp, err := c.doRequest(ctx, url, &res, http.MethodGet, q, nil)
	if err != nil {
		return nil, "", nil, err
	}

	if res.NextPageToken != "" {
		return res.Groups, res.NextPageToken, resp, nil
	}

	return res.Groups, "", resp, nil
}

// GetRoles returns all Zoom roles.
func (c *Client) GetRoles(ctx context.Context) ([]Role, *http.Response, error) {
	url := fmt.Sprint(c.baseURL, "/roles")
	var res struct {
		Roles []Role `json:"roles"`
	}

	resp, err := c.doRequest(ctx, url, &res, http.MethodGet, nil, nil)
	if err != nil {
		return nil, nil, err
	}

	return res.Roles, resp, nil
}

// GetGroupMembers returns one page of Zoom group members.
func (c *Client) GetGroupMembers(ctx context.Context, groupId string, nextToken string) ([]User, string, *http.Response, error) {
	url := fmt.Sprintf("%s/groups/%s/members", c.baseURL, groupId)
	var res struct {
		PaginationData
		Members []User `json:"members"`
	}

	q := paginationQuery(nextToken)
	resp, err := c.doRequest(ctx, url, &res, http.MethodGet, q, nil)
	if err != nil {
		return nil, "", nil, err
	}

	return res.Members, res.NextPageToken, resp, nil
}

// GetGroupAdmins returns one page of Zoom group admins.
func (c *Client) GetGroupAdmins(ctx context.Context, groupId string, nextToken string) ([]User, string, *http.Response, error) {
	url := fmt.Sprintf("%s/groups/%s/admins", c.baseURL, groupId)
	var res struct {
		PaginationData
		Admins []User `json:"admins"`
	}

	q := paginationQuery(nextToken)
	resp, err := c.doRequest(ctx, url, &res, http.MethodGet, q, nil)
	if err != nil {
		return nil, "", nil, err
	}

	return res.Admins, res.NextPageToken, resp, nil
}

// GetContactGroupMembers returns all Zoom contact group members.
func (c *Client) GetContactGroupMembers(ctx context.Context, groupId string, nextToken string) ([]GroupMember, string, *http.Response, error) {
	url := fmt.Sprintf("%s/contacts/groups/%s/members", c.baseURL, groupId)
	var res struct {
		PaginationData
		Members []GroupMember `json:"group_members"`
	}

	q := paginationQuery(nextToken)
	resp, err := c.doRequest(ctx, url, &res, http.MethodGet, q, nil)
	if err != nil {
		return nil, "", nil, err
	}

	if res.NextPageToken != "" {
		return res.Members, res.NextPageToken, resp, nil
	}

	return res.Members, "", resp, nil
}

// GetRoleMembers returns all Zoom role members.
func (c *Client) GetRoleMembers(ctx context.Context, roleId string, nextToken string) ([]User, string, *http.Response, error) {
	url := fmt.Sprintf("%s/roles/%s/members", c.baseURL, roleId)
	var res struct {
		PaginationData
		Members []User `json:"members"`
	}

	q := paginationQuery(nextToken)
	resp, err := c.doRequest(ctx, url, &res, http.MethodGet, q, nil)
	if err != nil {
		return nil, "", nil, err
	}

	if res.NextPageToken != "" {
		return res.Members, res.NextPageToken, resp, nil
	}

	return res.Members, "", resp, nil
}

// GetUser returns user details.
func (c *Client) GetUser(ctx context.Context, userId string) (User, *http.Response, error) {
	// Escape before joining so JoinPath cannot resolve dot segments embedded
	// in the opaque user ID.
	requestURL, err := url.JoinPath(c.baseURL, "users", url.PathEscape(userId))
	if err != nil {
		return User{}, nil, fmt.Errorf("failed to build user URL: %w", err)
	}

	var res User
	resp, err := c.doRequest(ctx, requestURL, &res, http.MethodGet, nil, nil)
	if err != nil {
		return User{}, nil, err
	}

	return res, resp, nil
}

// AddGroupMembers adds user to a group.
func (c *Client) AddGroupMembers(ctx context.Context, groupId, userId string) error {
	url := fmt.Sprint(c.baseURL, "/groups/", groupId, "/members")
	members := []Payload{
		{
			ID: userId,
		},
	}

	requestBody, err := json.Marshal(map[string]any{
		"members": members,
	})
	if err != nil {
		return err
	}

	var res struct {
		MemberIDs []string `json:"member_ids"`
	}
	resp, e := c.doRequest(ctx, url, &res, http.MethodPost, nil, requestBody)
	if e != nil {
		return e
	}

	defer resp.Body.Close()

	return nil
}

// AddGroupAdmins adds admin to the group.
func (c *Client) AddGroupAdmins(ctx context.Context, groupId, userId string) error {
	url := fmt.Sprint(c.baseURL, "/groups/", groupId, "/admins")
	members := []Payload{
		{
			ID: userId,
		},
	}

	requestBody, err := json.Marshal(map[string]any{
		"admins": members,
	})
	if err != nil {
		return err
	}

	var res struct {
		MemberIDs []string `json:"member_ids"`
	}
	resp, e := c.doRequest(ctx, url, &res, http.MethodPost, nil, requestBody)
	if e != nil {
		return e
	}

	defer resp.Body.Close()

	return nil
}

// DeleteGroupAdmin removes admin from the group.
func (c *Client) DeleteGroupAdmin(ctx context.Context, groupId, userId string) error {
	url := fmt.Sprint(c.baseURL, "/groups/", groupId, "/admins/", userId)

	resp, err := c.doRequest(ctx, url, nil, http.MethodDelete, nil, nil)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	return nil
}

// DeleteGroupMember removes member from the group.
func (c *Client) DeleteGroupMember(ctx context.Context, groupId, userId string) error {
	url := fmt.Sprint(c.baseURL, "/groups/", groupId, "/members/", userId)

	resp, err := c.doRequest(ctx, url, nil, http.MethodDelete, nil, nil)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	return nil
}

// AssignRole assigns role to a user.
func (c *Client) AssignRole(ctx context.Context, roleId, userId string) error {
	url := fmt.Sprint(c.baseURL, "/roles/", roleId, "/members")
	members := []Payload{
		{
			ID: userId,
		},
	}

	requestBody, err := json.Marshal(map[string]any{
		"members": members,
	})

	if err != nil {
		return err
	}

	var res struct {
		AddAt string `json:"add_at"`
		IDs   string `json:"ids"`
	}
	resp, e := c.doRequest(ctx, url, &res, http.MethodPost, nil, requestBody)
	if e != nil {
		return e
	}

	defer resp.Body.Close()
	return nil
}

// UnassignRole unassigns role from a user.
func (c *Client) UnassignRole(ctx context.Context, roleId, userId string) error {
	url := fmt.Sprint(c.baseURL, "/roles/", roleId, "/members/", userId)

	resp, err := c.doRequest(ctx, url, nil, http.MethodDelete, nil, nil)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	return nil
}

func (c *Client) CreateUser(ctx context.Context, newUser *UserCreationBody) (*UserCreationResponse, error) {
	requestURL, err := url.JoinPath(c.baseURL, "users")
	if err != nil {
		return nil, err
	}

	requestBody, err := json.Marshal(newUser)
	if err != nil {
		return nil, err
	}

	var res UserCreationResponse
	resp, err := c.doRequest(ctx, requestURL, &res, http.MethodPost, nil, requestBody)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	return &res, nil
}

func (c *Client) DeleteUser(ctx context.Context, userId string) error {
	return c.DeleteUserWithTransfer(ctx, userId, DeleteUserOptions{})
}

// DeleteUserOptions configures the query parameters DELETE /v2/users/{userId}
// accepts for reassigning a user's meetings, webinars, and cloud recordings to
// another user (TransferEmail) as part of removing them from the account.
type DeleteUserOptions struct {
	// Action is Disassociate (unlink the user from the account) or Delete
	// (permanently remove the user). Empty defers to Zoom's default
	// (disassociate).
	Action            DeleteAction
	TransferEmail     string
	TransferMeeting   bool
	TransferWebinar   bool
	TransferRecording bool
}

// DeleteUserWithTransfer removes a user via DELETE /v2/users/{userId},
// optionally transferring their meetings, webinars, and cloud recordings to
// opts.TransferEmail first. Zoom requires TransferEmail whenever any of the
// transfer flags is set; the caller is responsible for that validation.
func (c *Client) DeleteUserWithTransfer(ctx context.Context, userId string, opts DeleteUserOptions) error {
	// See GetUser: escape the opaque ID before joining so dot segments cannot
	// redirect the request to another Zoom endpoint.
	requestURL, err := url.JoinPath(c.baseURL, "users", url.PathEscape(userId))
	if err != nil {
		return fmt.Errorf("failed to build delete user URL: %w", err)
	}

	var params url.Values
	if opts.Action != "" || opts.TransferEmail != "" || opts.TransferMeeting || opts.TransferWebinar || opts.TransferRecording {
		params = url.Values{}
		if opts.Action != "" {
			params.Set("action", string(opts.Action))
		}
		if opts.TransferEmail != "" {
			params.Set("transfer_email", opts.TransferEmail)
		}
		if opts.TransferMeeting {
			params.Set("transfer_meeting", "true")
		}
		if opts.TransferWebinar {
			params.Set("transfer_webinar", "true")
		}
		if opts.TransferRecording {
			params.Set("transfer_recording", "true")
		}
	}

	resp, err := c.doRequest(ctx, requestURL, nil, http.MethodDelete, params, nil)
	if err != nil {
		return err
	}

	defer resp.Body.Close()
	return nil
}

// PatchUserLicense updates a user's license tier via PATCH /v2/users/{userId}.
// Zoom returns 204 No Content on success and applies any seat consumption or
// release immediately.
func (c *Client) PatchUserLicense(ctx context.Context, userId string, licenseType UserType) error {
	requestURL, err := url.JoinPath(c.baseURL, "users", url.PathEscape(userId))
	if err != nil {
		return fmt.Errorf("failed to build patch user URL: %w", err)
	}

	requestBody, err := json.Marshal(UserPatchBody{Type: licenseType})
	if err != nil {
		return err
	}

	resp, err := c.doRequest(ctx, requestURL, nil, http.MethodPatch, nil, requestBody)
	if err != nil {
		return err
	}

	defer resp.Body.Close()
	return nil
}

// GetAccountPlanUsage returns the base plan's purchased and consumed seat
// counts from GET /v2/accounts/me/plans/usage. Requires the
// `billing:read:plan_usage:admin` scope (or the legacy `billing:read`).
func (c *Client) GetAccountPlanUsage(ctx context.Context) (*PlanUsage, *http.Response, error) {
	requestURL := fmt.Sprint(c.baseURL, "/accounts/me/plans/usage")
	var res PlanUsage

	response, err := c.doRequest(ctx, requestURL, &res, http.MethodGet, nil, nil)
	if err != nil {
		return nil, nil, err
	}

	return &res, response, nil
}

// doRequest sends one Zoom API request through uhttp and decodes its response.
func (c *Client) doRequest(
	ctx context.Context,
	rawURL string,
	res any,
	method string,
	params url.Values,
	payload []byte,
) (*http.Response, error) {
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
		// The client historically performed uncached reads. Keep list and
		// provisioning results fresh when a long-lived connector syncs after
		// a mutation.
		requestOptions = append(requestOptions, uhttp.WithNoCache())
	}
	if payload != nil {
		requestOptions = append(requestOptions, uhttp.WithBody(payload), uhttp.WithContentTypeJSONHeader())
	}
	req, err := c.httpClient.NewRequest(ctx, method, requestURL, requestOptions...)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	apiErr := &APIError{}
	doOptions := []uhttp.DoOption{
		withZoomErrorResponse(apiErr),
	}
	if res != nil {
		doOptions = append(doOptions, withZoomJSONResponse(res))
	}

	return c.httpClient.Do(req, doOptions...)
}
