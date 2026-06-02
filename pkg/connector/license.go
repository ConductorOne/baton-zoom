package connector

import (
	"context"
	"fmt"
	"strconv"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	grant "github.com/conductorone/baton-sdk/pkg/types/grant"
	"github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-zoom/pkg/zoom"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

const (
	assignedEntitlement = "assigned"
)

// licenseDefinition describes one of Zoom's three license tiers. Zoom does
// not expose a "list licenses" endpoint — the tiers are fixed and the API
// references them by integer code on the User.type field.
type licenseDefinition struct {
	id   zoom.UserType
	name string
}

// licenseDefinitions enumerates the tiers we model. Order is preserved when
// rendering List results for deterministic output.
var licenseDefinitions = []licenseDefinition{
	{id: zoom.BasicUser, name: "Basic"},
	{id: zoom.LicensedUser, name: "Licensed"},
	{id: zoom.OnPremUser, name: "On-Prem"},
}

type licenseResourceType struct {
	resourceType      *v2.ResourceType
	client            *zoom.Client
	syncInactiveUsers bool
}

func (l *licenseResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return l.resourceType
}

// licenseResource builds a connector resource for a single Zoom license
// tier. Purchased and consumed seat counts come from
// GET /v2/accounts/me/plans/usage and are only meaningful for the Licensed
// tier (Basic seats are free and On-Prem is provisioned outside Zoom Cloud).
func licenseResource(def licenseDefinition, purchased, consumed int64) (*v2.Resource, error) {
	licenseOpts := []resource.LicenseProfileTraitOption{
		resource.WithLicenseName(def.name),
	}

	if def.id == zoom.LicensedUser && purchased > 0 {
		licenseOpts = append(licenseOpts, resource.WithLicenseSeats(purchased, consumed))
	}

	return resource.NewResource(
		def.name,
		resourceTypeLicense,
		strconv.Itoa(int(def.id)),
		resource.WithLicenseProfileTrait(licenseOpts...),
	)
}

// List returns the three static license tiers. Seat counts for the Licensed
// tier are best-effort: if the billing scope is missing or the call fails
// the tier is still emitted, just without seat numbers. This keeps sync
// usable for accounts where the operator hasn't (yet) granted
// billing:read:plan_usage:admin.
func (l *licenseResourceType) List(ctx context.Context, _ *v2.ResourceId, _ resource.SyncOpAttrs) ([]*v2.Resource, *resource.SyncOpResults, error) {
	logger := ctxzap.Extract(ctx)

	var purchased, consumed int64
	usage, resp, err := l.client.GetAccountPlanUsage(ctx)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		// Treat plan-usage failures as non-fatal: seat counts are an
		// observability nicety, not required for grant evaluation.
		logger.Warn(
			"baton-zoom: failed to fetch plan usage; emitting licenses without seat counts",
			zap.Error(err),
		)
	} else if usage != nil {
		purchased = int64(usage.PlanBase.Hosts)
		consumed = int64(usage.PlanBase.Usage)
	}

	rv := make([]*v2.Resource, 0, len(licenseDefinitions))
	for _, def := range licenseDefinitions {
		lr, err := licenseResource(def, purchased, consumed)
		if err != nil {
			return nil, nil, fmt.Errorf("baton-zoom: failed to build license resource %s: %w", def.name, err)
		}
		rv = append(rv, lr)
	}

	annos, err := parseResp(resp)
	if err != nil {
		return nil, nil, err
	}

	return rv, &resource.SyncOpResults{Annotations: annos}, nil
}

// Entitlements emits a single "assigned" assignment-purpose entitlement per
// license tier. Each Zoom user holds exactly one license at a time, so
// grants are mutually exclusive across tiers but expressed as independent
// entitlements per resource.
func (l *licenseResourceType) Entitlements(_ context.Context, res *v2.Resource, _ resource.SyncOpAttrs) ([]*v2.Entitlement, *resource.SyncOpResults, error) {
	opts := []ent.EntitlementOption{
		ent.WithGrantableTo(resourceTypeUser),
		ent.WithDisplayName(fmt.Sprintf("%s license %s", res.DisplayName, assignedEntitlement)),
		ent.WithDescription(fmt.Sprintf("Holds a %s license seat in Zoom", res.DisplayName)),
	}

	en := ent.NewAssignmentEntitlement(res, assignedEntitlement, opts...)
	return []*v2.Entitlement{en}, &resource.SyncOpResults{}, nil
}

// Grants paginates over Zoom users (active, plus inactive when configured)
// and emits one grant per user whose User.type matches the license tier
// being synced. The same status-stack pagination pattern used by userResourceType
// is used here to avoid an extra GET /v2/users/{id} per user.
func (l *licenseResourceType) Grants(ctx context.Context, res *v2.Resource, opts resource.SyncOpAttrs) ([]*v2.Grant, *resource.SyncOpResults, error) {
	licenseID, err := strconv.Atoi(res.Id.Resource)
	if err != nil {
		return nil, nil, fmt.Errorf("baton-zoom: invalid license resource id %q: %w", res.Id.Resource, err)
	}

	bag := &pagination.Bag{}
	if err := bag.Unmarshal(opts.PageToken.Token); err != nil {
		return nil, nil, err
	}

	if bag.Current() == nil {
		if l.syncInactiveUsers {
			bag.Push(pagination.PageState{ResourceTypeID: resourceTypeLicense.Id, ResourceID: userStatusInactive})
		}
		bag.Push(pagination.PageState{ResourceTypeID: resourceTypeLicense.Id, ResourceID: userStatusActive})
	}

	status := bag.Current().ResourceID
	page := bag.PageToken()

	users, nextPage, resp, err := l.client.GetUsers(ctx, page, status)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, nil, err
	}
	defer resp.Body.Close()

	if err := bag.Next(nextPage); err != nil {
		return nil, nil, err
	}

	pageToken, err := bag.Marshal()
	if err != nil {
		return nil, nil, err
	}

	annos, err := parseResp(resp)
	if err != nil {
		return nil, nil, err
	}

	var rv []*v2.Grant
	for _, u := range users {
		if u.Type != licenseID {
			continue
		}
		principalID := &v2.ResourceId{
			ResourceType: resourceTypeUser.Id,
			Resource:     u.ID,
		}
		rv = append(rv, grant.NewGrant(res, assignedEntitlement, principalID))
	}

	return rv, &resource.SyncOpResults{NextPageToken: pageToken, Annotations: annos}, nil
}

// Grant applies a license by PATCHing the user's `type` field.
//
// Idempotency follows the "Role Idempotency via User State" pattern from the
// baton-provisioning skill: a pre-flight GET resolves the user's current tier
// and short-circuits with GrantAlreadyExists when the target tier already
// matches, avoiding an unnecessary PATCH.
//
// Every other rejection (deactivated user, missing scope, malformed payload,
// etc.) is left to the Zoom API and propagated verbatim. The connector does
// not pre-judge user state — Zoom is the source of truth.
func (l *licenseResourceType) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) ([]*v2.Grant, annotations.Annotations, error) {
	logger := ctxzap.Extract(ctx)

	if principal.Id.ResourceType != resourceTypeUser.Id {
		logger.Warn(
			"baton-zoom: only users can be granted a license",
			zap.String("principal_type", principal.Id.ResourceType),
			zap.String("principal_id", principal.Id.Resource),
		)
		return nil, nil, fmt.Errorf("baton-zoom: only users can be granted a license")
	}

	targetType, err := strconv.Atoi(entitlement.Resource.Id.Resource)
	if err != nil {
		return nil, nil, fmt.Errorf("baton-zoom: invalid license resource id %q: %w", entitlement.Resource.Id.Resource, err)
	}

	user, resp, err := l.client.GetUser(ctx, principal.Id.Resource)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, nil, fmt.Errorf("baton-zoom: failed to get user before granting license: %w", err)
	}
	resp.Body.Close()

	if user.Type == targetType {
		return nil, annotations.New(&v2.GrantAlreadyExists{}), nil
	}

	if err := l.client.PatchUserLicense(ctx, principal.Id.Resource, zoom.UserType(targetType)); err != nil {
		return nil, nil, fmt.Errorf("baton-zoom: failed to assign license to user: %w", err)
	}

	return nil, nil, nil
}

// Revoke downgrades the user to Basic via PATCH `type=1` (Zoom has no
// "no license" state — Basic is the floor tier).
//
// Returns GrantAlreadyRevoked without an API call when:
//   1. The user no longer holds the tier being revoked.
//   2. The tier being revoked is Basic — no lower tier exists, so it's a no-op.
//
// All other Zoom errors are propagated verbatim.
func (l *licenseResourceType) Revoke(ctx context.Context, g *v2.Grant) (annotations.Annotations, error) {
	logger := ctxzap.Extract(ctx)

	entitlement := g.Entitlement
	principal := g.Principal

	if principal.Id.ResourceType != resourceTypeUser.Id {
		logger.Warn(
			"baton-zoom: only users can have a license revoked",
			zap.String("principal_type", principal.Id.ResourceType),
			zap.String("principal_id", principal.Id.Resource),
		)
		return nil, fmt.Errorf("baton-zoom: only users can have a license revoked")
	}

	grantedType, err := strconv.Atoi(entitlement.Resource.Id.Resource)
	if err != nil {
		return nil, fmt.Errorf("baton-zoom: invalid license resource id %q: %w", entitlement.Resource.Id.Resource, err)
	}

	user, resp, err := l.client.GetUser(ctx, principal.Id.Resource)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, fmt.Errorf("baton-zoom: failed to get user before revoking license: %w", err)
	}
	resp.Body.Close()

	if user.Type != grantedType {
		return annotations.New(&v2.GrantAlreadyRevoked{}), nil
	}

	if grantedType == int(zoom.BasicUser) {
		return annotations.New(&v2.GrantAlreadyRevoked{}), nil
	}

	if err := l.client.PatchUserLicense(ctx, principal.Id.Resource, zoom.BasicUser); err != nil {
		return nil, fmt.Errorf("baton-zoom: failed to revoke license from user: %w", err)
	}

	return nil, nil
}

func licenseBuilder(client *zoom.Client, syncInactiveUsers bool) *licenseResourceType {
	return &licenseResourceType{
		resourceType:      resourceTypeLicense,
		client:            client,
		syncInactiveUsers: syncInactiveUsers,
	}
}
