package connector

import (
	"context"
	"fmt"
	"strings"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	firstNameKey       = "first_name"
	lastNameKey        = "last_name"
	displayNameKey     = "display_name"
	emailKey           = "email"
	loginKey           = "login"
	userIDKey          = "user_id"
	userTypeProfileKey = "type"

	userStatusActive   = "active"
	userStatusInactive = "inactive"
	userStatusPending  = "pending"

	memberEntitlement   = "member"
	adminEntitlement    = "admin"
	assignedEntitlement = "assigned"

	contactMemberTypeUser = 1
)

func userTraitStatus(status string) v2.Status_ResourceStatus {
	switch status {
	case userStatusActive:
		return v2.Status_RESOURCE_STATUS_ENABLED
	case userStatusInactive:
		return v2.Status_RESOURCE_STATUS_DISABLED
	case userStatusPending:
		return v2.Status_RESOURCE_STATUS_PENDING
	default:
		return v2.Status_RESOURCE_STATUS_UNSPECIFIED
	}
}

func parsePageToken(i string, resourceID *v2.ResourceId, operation string) (*pagination.Bag, string, error) {
	b := &pagination.Bag{}
	err := b.Unmarshal(i)
	if err != nil {
		return nil, "", uhttp.WrapErrors(
			codes.InvalidArgument,
			fmt.Sprintf("baton-zoom: %s: invalid page token", operation),
			err,
		)
	}
	if b.Current() == nil {
		b.Push(pagination.PageState{
			ResourceTypeID: resourceID.ResourceType,
			ResourceID:     resourceID.Resource,
		})
	}
	return b, b.PageToken(), nil
}

func willSyncResourceType(syncResourceTypes map[string]struct{}, resourceTypeID string) bool {
	if len(syncResourceTypes) == 0 {
		return true
	}
	_, ok := syncResourceTypes[resourceTypeID]
	return ok
}

func nextBagToken(bag *pagination.Bag, nextToken string) (string, error) {
	if nextToken == "" {
		return "", nil
	}
	return bag.NextToken(nextToken)
}

func requireUserPrincipal(ctx context.Context, principal *v2.Resource, message string) error {
	if principal.Id.ResourceType == resourceTypeUser.Id {
		return nil
	}
	ctxzap.Extract(ctx).Debug(
		message,
		zap.String("principal_type", principal.Id.ResourceType),
		zap.String("principal_id", principal.Id.Resource),
	)
	return status.Error(codes.InvalidArgument, message)
}

func groupEntitlementSlug(entitlementID string) (string, error) {
	parts := strings.Split(entitlementID, ":")
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid entitlement ID format %q", entitlementID)
	}
	slug := parts[len(parts)-1]
	switch slug {
	case memberEntitlement, adminEntitlement:
		return slug, nil
	default:
		return "", fmt.Errorf("unknown group entitlement %q (valid: %s, %s)", slug, memberEntitlement, adminEntitlement)
	}
}

func primaryEmail(res *v2.Resource) string {
	userTrait, err := resource.GetUserTrait(res)
	if err != nil || userTrait == nil {
		return ""
	}
	for _, email := range userTrait.GetEmails() {
		if email.GetIsPrimary() {
			return email.GetAddress()
		}
	}
	if emails := userTrait.GetEmails(); len(emails) > 0 {
		return emails[0].GetAddress()
	}
	return ""
}
