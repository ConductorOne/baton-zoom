package connector

import (
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
)

// Zoom API granular scopes referenced by the resource type capability annotations.
// Server-to-Server OAuth apps created today must use granular scopes; the classic
// `user:read:admin` / `user:write:admin` are no longer accepted.
//
// Note: Zoom splits user writes into distinct granular scopes —
// `user:write:user:admin` only authorizes POST /v2/users (create);
// PATCH /v2/users/{userId} (used for license tier updates) requires
// the separate `user:update:user:admin` scope.
const (
	scopeUserReadList = "user:read:list_users:admin"
	scopeUserRead     = "user:read:user:admin"
	scopeUserWrite    = "user:write:user:admin"
	scopeUserUpdate   = "user:update:user:admin"
	scopeUserDelete   = "user:delete:user:admin"
	scopeBillingRead  = "billing:read:plan_usage:admin"
)

func capabilityPermissions(perms ...string) *v2.CapabilityPermissions {
	cp := &v2.CapabilityPermissions{}
	for _, p := range perms {
		cp.Permissions = append(cp.Permissions, &v2.CapabilityPermission{Permission: p})
	}
	return cp
}

var (
	resourceTypeUser = &v2.ResourceType{
		Id:          "user",
		DisplayName: "User",
		Traits: []v2.ResourceType_Trait{
			v2.ResourceType_TRAIT_USER,
		},
		Annotations: annotations.New(
			capabilityPermissions(
				scopeUserRead,
				scopeUserReadList,
				scopeUserWrite,
				scopeUserDelete,
			),
			&v2.SkipEntitlements{},
		),
	}

	resourceTypeGroup = &v2.ResourceType{
		Id:          "group",
		DisplayName: "Group",
		Traits: []v2.ResourceType_Trait{
			v2.ResourceType_TRAIT_GROUP,
		},
		Annotations: annotations.New(
			capabilityPermissions(
				"group:read:list_groups:admin",
				"group:read:list_members:admin",
				"group:read:administrator:admin",
				"group:write:member:admin",
				"group:delete:member:admin",
				"group:write:administrator:admin",
				"group:delete:administrator:admin",
			),
		),
	}

	resourceTypeContactGroup = &v2.ResourceType{
		Id:          "contactGroup",
		DisplayName: "Contact Group",
		Traits: []v2.ResourceType_Trait{
			v2.ResourceType_TRAIT_GROUP,
		},
		Annotations: annotations.New(
			capabilityPermissions(
				"contact_group:read:list_groups:admin",
				"contact_group:read:list_members:admin",
			),
		),
	}

	resourceTypeInvite = &v2.ResourceType{
		Id:          "invite",
		DisplayName: "Invite",
		Traits: []v2.ResourceType_Trait{
			v2.ResourceType_TRAIT_USER,
		},
		Annotations: annotations.New(
			capabilityPermissions(scopeUserReadList),
			&v2.SkipEntitlementsAndGrants{},
			&v2.SkipSyncAnomalyDetection{},
		),
	}

	resourceTypeRole = &v2.ResourceType{
		Id:          "role",
		DisplayName: "Role",
		Traits: []v2.ResourceType_Trait{
			v2.ResourceType_TRAIT_ROLE,
		},
		Annotations: annotations.New(
			capabilityPermissions(
				"role:read:list_roles:admin",
				"role:read:list_members:admin",
				"role:write:member:admin",
				"role:delete:member:admin",
			),
		),
	}

	resourceTypeLicense = &v2.ResourceType{
		Id:          "license",
		DisplayName: "License",
		Traits: []v2.ResourceType_Trait{
			v2.ResourceType_TRAIT_LICENSE_PROFILE,
		},
		Annotations: annotations.New(
			capabilityPermissions(
				scopeUserReadList,
				scopeUserRead,
				scopeUserUpdate,
				scopeBillingRead,
			),
			&v2.SkipGrants{},
		),
	}
)
