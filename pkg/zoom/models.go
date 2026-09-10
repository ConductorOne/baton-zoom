package zoom

import "github.com/conductorone/baton-sdk/pkg/uhttp"

type ActionType string
type UserType int

const (
	CreateUser     ActionType = "create"
	AutoCreateUser ActionType = "autoCreate"
	CustCreateUser ActionType = "custCreate"
	SSOCreate      ActionType = "ssoCreate"

	// Zoom user license tiers exposed by the `type` field on GET /v2/users.
	// The API enum is 1, 2, 4, 99; type=99 (None) is settable only via the
	// ssoCreate action (never via PATCH), so only the PATCH-assignable tiers
	// are modeled here. Source: https://developers.zoom.us/docs/api/users/
	BasicUser      UserType = 1
	LicensedUser   UserType = 2
	UnassignedUser UserType = 4 // "Unassigned without Meetings Basic" (aka No Meetings License)
)

// DeleteAction selects the outcome of DELETE /v2/users/{userId}: unlink the
// user from the account (Disassociate) while keeping the Zoom user record, or
// remove the user entirely (Delete).
type DeleteAction string

const (
	Disassociate DeleteAction = "disassociate"
	Delete       DeleteAction = "delete"
)

// APIError preserves HTTP response metadata while decoding Zoom's
// structured error code and message.
type APIError struct {
	StatusCode int    `json:"-"`
	Body       string `json:"-"`
	Code       int    `json:"code"`
	Msg        string `json:"message"`
}

// OAuthError is Zoom's OAuth token error envelope.
type OAuthError struct {
	StatusCode int    `json:"-"`
	Body       string `json:"-"`
	Code       string `json:"error"`
	Reason     string `json:"reason"`
}

// Client calls the Zoom REST API through Baton's authenticated HTTP client.
type Client struct {
	httpClient *uhttp.BaseHttpClient
	token      string
	baseURL    string
}

type accessTokenResponse struct {
	AccessToken string `json:"access_token"`
}

type Payload struct {
	ID string `json:"id"`
}

// membershipMutationResponse is Zoom's reply to a group member or admin add.
// IDs lists only the ids the call actually created, so it is empty whenever
// the call created nothing.
type membershipMutationResponse struct {
	IDs string `json:"ids"`
}

// PaginationData contains Zoom's standard page metadata.
type PaginationData struct {
	NextPageToken string `json:"next_page_token"`
	PageSize      int    `json:"page_size"`
	TotalRecords  int    `json:"total_records"`
}

type usersResponse struct {
	PaginationData
	Users []*User `json:"users"`
}

type Group struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type groupsResponse struct {
	PaginationData
	Groups []*Group `json:"groups"`
}

type contactGroupsResponse struct {
	PaginationData
	Groups []*ContactGroup `json:"groups"`
}

type adminsResponse struct {
	PaginationData
	Admins []*User `json:"admins"`
}

type Role struct {
	Description string `json:"description"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
}

type rolesResponse struct {
	Roles []*Role `json:"roles"`
}

type User struct {
	ID          string   `json:"id"`
	Email       string   `json:"email"`
	FirstName   string   `json:"first_name"`
	LastName    string   `json:"last_name"`
	RoleName    string   `json:"role_name"`
	Type        int      `json:"type"`
	DisplayName string   `json:"display_name"`
	RoleID      string   `json:"role_id"`
	Status      string   `json:"status"`
	GroupIDs    []string `json:"group_ids"`
}

type UserCreationBody struct {
	Action ActionType `json:"action"`
	// The indicated Action could be:
	//  - create - The user receives an email from Zoom containing a confirmation link. The user must then use the link to activate their Zoom account.
	// The user can then set or change their password.
	//  - autoCreate - This action is for Enterprise customers with a managed domain.
	// autoCreate creates an email login type for users.
	//  - custCreate - Users created with this action do not have passwords and will not have the ability to log into the Zoom web portal or the Zoom client.
	// These users can still host and join meetings using the start_url and join_url respectively. To use this option, you must contact the Integrated Software Vendor (ISV) sales team.
	//  - ssoCreate - This action is provided for the enabled “Pre-provisioning SSO User” option. A user created this way has no password.
	// If it is not a Basic user, a personal vanity URL with the username (no domain) of the provisioning email is generated.
	// If the username or PMI is invalid or occupied, it uses a random number or random personal vanity URL.

	UserInfo UserCreationInfo `json:"user_info"`
}

type UserCreationInfo struct {
	Email       string `json:"email"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	DisplayName string `json:"display_name"`
	// Password    string   `json:"password"`
	Type UserType `json:"type"`
}

type UserCreationResponse struct {
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	Id        string `json:"id"`
	LastName  string `json:"last_name"`
	Type      int    `json:"type"`
}

type ContactGroup struct {
	ID          string `json:"group_id"`
	Name        string `json:"group_name"`
	Privacy     int64  `json:"group_privacy"`
	Description string `json:"description"`
}

type GroupMember struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type int    `json:"type"`
}

type contactGroupMembersResponse struct {
	PaginationData
	Members []*GroupMember `json:"group_members"`
}

// DeleteUserOptions configures the removal action and optional ownership transfer.
type DeleteUserOptions struct {
	// Empty uses Zoom's default action, Disassociate.
	Action            DeleteAction
	TransferEmail     string
	TransferMeeting   bool
	TransferWebinar   bool
	TransferRecording bool
}

// PlanBase describes the base plan slot from GET /v2/accounts/me/plans/usage.
// Hosts is the number of purchased Licensed seats; Usage is the number of
// consumed Licensed seats. Pending is the count queued by an admin but not
// yet applied.
type PlanBase struct {
	Type    string `json:"type"`
	Hosts   int    `json:"hosts"`
	Usage   int    `json:"usage"`
	Pending int    `json:"pending"`
}

// PlanUsage is the minimal projection of the plan usage payload that this
// connector consumes. Zoom returns many additional fields (recording, room,
// webinar, etc.) that we deliberately ignore.
type PlanUsage struct {
	PlanBase PlanBase `json:"plan_base"`
}

// UserPatchBody is the body sent to PATCH /v2/users/{userId} when changing a
// user's license tier. A 204 No Content response indicates success.
type UserPatchBody struct {
	Type UserType `json:"type"`
}
