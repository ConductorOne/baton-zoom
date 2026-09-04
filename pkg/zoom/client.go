package zoom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
)

type Client struct {
	httpClient *http.Client
	token      string
	baseURL    string
}

// APIError wraps a non-2xx Zoom API response so callers can inspect the
// status code, e.g. to treat a 404 on delete as "already gone".
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("request failed with status code %d: %s", e.StatusCode, e.Body)
}

const (
	defaultBaseURL   = "https://api.zoom.us/v2"
	defaultAuthURL   = "https://zoom.us/oauth/token"
	resourcePageSize = "50"
)

func NewClient(httpClient *http.Client, token string, baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	// Trim trailing slashes so every "c.baseURL + "/resource/" + id" call
	// site (and url.PathEscape-based ones) gets a single "/" between them,
	// regardless of how the operator-settable --base-url flag was entered.
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		httpClient: httpClient,
		token:      token,
		baseURL:    baseURL,
	}
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
	httpClient, err := uhttp.NewClient(ctx, uhttp.WithLogger(true, ctxzap.Extract(ctx)))
	if err != nil {
		return "", err
	}

	data := url.Values{}
	data.Add("account_id", accountId)
	data.Add("grant_type", "account_credentials")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, defaultAuthURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Add("accept", "application/json")
	req.SetBasicAuth(clientId, clientSecret)
	req.URL.RawQuery = data.Encode()

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	var res struct {
		AccessToken string `json:"Access_token"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
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
	// PathEscape, not url.JoinPath: JoinPath resolves ../ segments in its
	// inputs, so a userId of "../accounts/me" would otherwise redirect this
	// request to a different Zoom endpoint entirely. PathEscape confines
	// userId to a single opaque path segment regardless of its content.
	requestURL := c.baseURL + "/users/" + url.PathEscape(userId)

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
	// See GetUser: PathEscape, not url.JoinPath, so a userId containing ../
	// can't redirect this request to a different Zoom endpoint.
	requestURL := c.baseURL + "/users/" + url.PathEscape(userId)

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
	requestURL, err := url.JoinPath(c.baseURL, "users", userId)
	if err != nil {
		return err
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

func (c *Client) doRequest(ctx context.Context, url string, res interface{}, method string, params url.Values, payload []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}

	if params != nil {
		req.URL.RawQuery = params.Encode()
	}

	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.token))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, err
	}

	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if len(b) == 0 && resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return resp, nil
	}

	if resp.StatusCode >= 400 {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(b)}
	}

	if err := json.Unmarshal(b, &res); err != nil {
		return nil, err
	}

	return resp, nil
}
