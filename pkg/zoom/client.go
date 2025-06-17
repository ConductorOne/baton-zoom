package zoom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
)

type Client struct {
	httpClient *http.Client
	token      string
}

const (
	baseUrl          = "https://api.zoom.us/v2"
	authUrl          = "https://zoom.us/oauth/token"
	resourcePageSize = "50"
)

func NewClient(httpClient *http.Client, token string) *Client {
	return &Client{
		httpClient: httpClient,
		token:      token,
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authUrl, nil)
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

// GetUsers returns all Zoom users.
func (c *Client) GetUsers(ctx context.Context, nextToken string) ([]User, string, *http.Response, error) {
	url := fmt.Sprint(baseUrl, "/users")
	var res struct {
		PaginationData
		Users []User `json:"users"`
	}

	q := paginationQuery(nextToken)
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
	url := fmt.Sprint(baseUrl, "/groups")
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
	url := fmt.Sprint(baseUrl, "/contacts/groups")
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
	url := fmt.Sprint(baseUrl, "/roles")
	var res struct {
		Roles []Role `json:"roles"`
	}

	resp, err := c.doRequest(ctx, url, &res, http.MethodGet, nil, nil)
	if err != nil {
		return nil, nil, err
	}

	return res.Roles, resp, nil
}

// GetGroupMembers returns all Zoom group members.
func (c *Client) GetGroupMembers(ctx context.Context, groupId string) ([]User, error) {
	url := fmt.Sprintf("%s/groups/%s/members", baseUrl, groupId)
	var token = ""
	var members []User

	for {
		var res struct {
			PaginationData
			Members []User `json:"members"`
		}

		q := paginationQuery(token)
		resp, err := c.doRequest(ctx, url, &res, http.MethodGet, q, nil)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		members = append(members, res.Members...)

		if res.NextPageToken == "" {
			break
		}

		token = res.NextPageToken
	}

	return members, nil
}

// GetGroupAdmins returns all Zoom group admins.
func (c *Client) GetGroupAdmins(ctx context.Context, groupId string) ([]User, error) {
	url := fmt.Sprintf("%s/groups/%s/admins", baseUrl, groupId)
	var admins []User
	var token = ""

	for {
		var res struct {
			PaginationData
			Admins []User `json:"admins"`
		}

		q := paginationQuery(token)
		resp, err := c.doRequest(ctx, url, &res, http.MethodGet, q, nil)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		admins = append(admins, res.Admins...)

		if res.NextPageToken == "" {
			break
		}

		token = res.NextPageToken
	}
	return admins, nil
}

// GetContactGroupMembers returns all Zoom contact group members.
func (c *Client) GetContactGroupMembers(ctx context.Context, groupId string, nextToken string) ([]GroupMember, string, *http.Response, error) {
	url := fmt.Sprintf("%s/contacts/groups/%s/members", baseUrl, groupId)
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
	url := fmt.Sprintf("%s/roles/%s/members", baseUrl, roleId)
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
	url := fmt.Sprint(baseUrl, "/users/", userId)
	var res User

	resp, err := c.doRequest(ctx, url, &res, http.MethodGet, nil, nil)
	if err != nil {
		return User{}, nil, err
	}

	return res, resp, nil
}

// AddGroupMembers adds user to a group.
func (c *Client) AddGroupMembers(ctx context.Context, groupId, userId string) error {
	url := fmt.Sprint(baseUrl, "/groups/", groupId, "/members")
	members := []Payload{
		{
			ID: userId,
		},
	}

	requestBody, err := json.Marshal(map[string]interface{}{
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
	url := fmt.Sprint(baseUrl, "/groups/", groupId, "/admins")
	members := []Payload{
		{
			ID: userId,
		},
	}

	requestBody, err := json.Marshal(map[string]interface{}{
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
	url := fmt.Sprint(baseUrl, "/groups/", groupId, "/admins/", userId)

	resp, err := c.doRequest(ctx, url, nil, http.MethodDelete, nil, nil)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	return nil
}

// DeleteGroupMember removes member from the group.
func (c *Client) DeleteGroupMember(ctx context.Context, groupId, userId string) error {
	url := fmt.Sprint(baseUrl, "/groups/", groupId, "/members/", userId)

	resp, err := c.doRequest(ctx, url, nil, http.MethodDelete, nil, nil)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	return nil
}

// AssignRole assigns role to a user.
func (c *Client) AssignRole(ctx context.Context, roleId, userId string) error {
	url := fmt.Sprint(baseUrl, "/roles/", roleId, "/members")
	members := []Payload{
		{
			ID: userId,
		},
	}

	requestBody, err := json.Marshal(map[string]interface{}{
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
	url := fmt.Sprint(baseUrl, "/roles/", roleId, "/members/", userId)

	resp, err := c.doRequest(ctx, url, nil, http.MethodDelete, nil, nil)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	return nil
}

func (c *Client) CreateUser(ctx context.Context, newUser *UserCreationBody) (*UserCreationResponse, error) {
	requestURL, err := url.JoinPath(baseUrl, "users")
	if err != nil {
		return nil, err
	}

	requestBody, err := json.Marshal(newUser)
	if err != nil {
		return nil, err
	}

	var res UserCreationResponse
	_, err = c.doRequest(ctx, requestURL, &res, http.MethodPost, nil, requestBody)
	if err != nil {
		return nil, err
	}

	return &res, nil
}

func (c *Client) DeleteUser(ctx context.Context, userId string) error {
	requestURL, err := url.JoinPath(baseUrl, "users", userId)
	if err != nil {
		return err
	}

	_, err = c.doRequest(ctx, requestURL, nil, http.MethodDelete, nil, nil)
	if err != nil {
		return err
	}

	return nil
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
		return nil, fmt.Errorf("request failed with status code %d: %s", resp.StatusCode, string(b))
	}

	if err := json.Unmarshal(b, &res); err != nil {
		return nil, err
	}

	return resp, nil
}
