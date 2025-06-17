package zoom

type ActionType string
type UserType int

const (
	CreateUser     ActionType = "create"
	AutoCreateUser ActionType = "autoCreate"
	CustCreateUser ActionType = "custCreate"
	SSOCreate      ActionType = "ssoCreate"

	BasicUser      UserType = 1
	LicensedUser   UserType = 2
	UnnasignedUser UserType = 3
	NoneUser       UserType = 99
)

type Group struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Pagination struct {
	NextPageToken string `json:"next_page_token"`
	PageSize      int    `json:"page_size"`
	TotalRecords  int    `json:"total_records"`
}

type Member struct {
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	ID        string `json:"id"`
	LastName  string `json:"last_name"`
	Type      int    `json:"type"`
}

type Role struct {
	Description string `json:"description"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
}

type User struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	RoleName    string `json:"role_name"`
	Type        int    `json:"type"`
	DisplayName string `json:"display_name"`
	RoleID      string `json:"role_id"`
	Status      string `json:"status"`
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
	// Feature     struct {
	//	ZoomPhone   bool `json:"zoom_phone"`
	//	ZoomOneType int  `json:"zoom_one_type"`
	// } `json:"feature"`
	// PlanUnitedType string `json:"plan_united_type"`
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
