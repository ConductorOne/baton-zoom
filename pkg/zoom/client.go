package zoom

import (
	"context"
	"encoding/json"
	"fmt"
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
	resp, err := c.doRequest(ctx, url, &res, q)
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
	resp, err := c.doRequest(ctx, url, &res, q)
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
	resp, err := c.doRequest(ctx, url, &res, q)
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

	resp, err := c.doRequest(ctx, url, &res, nil)
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
		resp, err := c.doRequest(ctx, url, &res, q)
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
		resp, err := c.doRequest(ctx, url, &res, q)
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
	resp, err := c.doRequest(ctx, url, &res, q)
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
	resp, err := c.doRequest(ctx, url, &res, q)
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

	resp, err := c.doRequest(ctx, url, &res, nil)
	if err != nil {
		return User{}, nil, err
	}

	return res, resp, nil
}

func (c *Client) doRequest(ctx context.Context, url string, res interface{}, params url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	if params != nil {
		req.URL.RawQuery = params.Encode()
	}

	req.Header.Add("accept", "application/json")
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.token))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	return resp, nil
}
