package zoom

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
