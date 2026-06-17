package connector

// License management for the Zoom connector.
//
// Endpoints used:
//   - GET   /v2/users                       - List users; User.type carries the per-user license tier
//   - GET   /v2/users/{userId}              - Pre-flight check before Grant/Revoke (idempotency)
//   - GET   /v2/accounts/me/plans/usage     - Account-level seat counts for the Licensed tier
//   - PATCH /v2/users/{userId}              - Change a user's tier (Grant / Revoke)
//
// Authentication: Server-to-Server OAuth, granular scopes:
//   - user:read:list_users:admin    - List users
//   - user:read:user:admin          - Get a single user (pre-flight before PATCH)
//   - user:update:user:admin        - PATCH a user's tier
//   - billing:read:plan_usage:admin - Plan usage (optional; enables Licensed seat counts)
//
// Official API references:
//   - Users API:        https://developers.zoom.us/docs/api/users/
//   - Update User:      https://developers.zoom.us/docs/api/users/#tag/users/patch/users/{userId}
//   - Plan Usage:       https://developers.zoom.us/docs/api/billing/ma/
//
// How Zoom licensing works (and how this connector models it):
//
//  1. License tier is a USER property, not a separate resource. Each user has
//     a `type` field on GET /v2/users with one of these values:
//
//       type=1  Basic      - Free tier, uncapped, anyone can sign up. Floor tier.
//       type=2  Licensed   - Paid base-plan seat (consumes 1 from plan_base).
//       type=4  Unassigned - "Unassigned without Meetings Basic" (aka No
//                            Meetings License): the user keeps the account
//                            seat but holds no meetings license. Settable via
//                            PATCH like any other tier.
//
//     The API enum also includes type=99 (None), but it is settable only at
//     creation via the ssoCreate action — never via PATCH /v2/users/{userId}
//     — so it cannot participate in the assign/unassign lifecycle and is not
//     modeled as a tier. Any value outside 1/2/4 emits no license grant.
//
//  2. Zoom does NOT expose a /licenses endpoint. The tiers are fixed and the
//     API references them only by integer code, so this connector models the
//     three assignable tiers (1, 2, 4) as STATIC resources hardcoded in
//     licenseDefinitions.
//
//  3. License grants are emitted PRINCIPAL-SIDE from userBuilder.Grants (using
//     User.type stashed in the user profile during List), not from
//     licenseResourceType.Grants. Each user holds exactly one tier, so a
//     single user pagination sweep produces all license grants — emitting
//     from the license side would require an O(N × tiers) scan.
//
//  4. Seat counts only attach to the Licensed tier resource:
//
//       plan_base.hosts = Licensed seats purchased (the cap)
//       plan_base.usage = Licensed seats consumed  (= count(User.type == 2))
//
//     Basic is uncapped (Zoom doesn't track it; nothing to surface).
//     Unassigned holds no meetings license, so there is no seat pool to surface.
//
//  5. Grant / Revoke both PATCH /v2/users/{userId} with {"type": N}. Revoke
//     downgrades to Basic because Zoom has no "no license" state — Basic is
//     the floor tier. Three short-circuits keep the operations idempotent
//     (each backed by a single pre-flight GET, no extra PATCH):
//
//       Grant of the user's current tier         → GrantAlreadyExists,  no PATCH
//       Revoke of a tier the user no longer holds → GrantAlreadyRevoked, no PATCH
//       Revoke of Basic (the floor tier)         → GrantAlreadyRevoked, no PATCH
//
// Out of scope (deliberately not modeled in C1):
//   - Add-on plan seats (plan_webinar, plan_large_meeting, plan_zoom_one,
//     plan_phone, plan_recording, ...): they don't change User.type and
//     aren't grantable as identity entitlements.
//   - Master account / reseller flows: /plans/usage returns plan_base as
//     an array there with active_hosts in place of usage. This connector
//     assumes the "accounts/me" sub-account shape.

import (
	"context"
	"fmt"
	"strconv"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-zoom/pkg/zoom"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

const (
	assignedEntitlement = "assigned"
)

// licenseDefinition is one Zoom license tier (id + display name).
type licenseDefinition struct {
	id   zoom.UserType
	name string
}

// licenseDefinitions is the static catalog of tiers we model. Order is
// preserved on List for deterministic output.
var licenseDefinitions = []licenseDefinition{
	{id: zoom.BasicUser, name: "Basic"},
	{id: zoom.LicensedUser, name: "Licensed"},
	{id: zoom.UnassignedUser, name: "Unassigned"},
}

type licenseResourceType struct {
	resourceType *v2.ResourceType
	client       *zoom.Client
}

func (l *licenseResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return l.resourceType
}

// licenseResource builds a connector resource for one license tier.
// Seat counts attach only to the Licensed tier. EntitlementIDs links
// the seat counts to the grants that consume them so C1 can map
// consumed seats to user grants.
func licenseResource(def licenseDefinition, purchased, consumed int64) (*v2.Resource, error) {
	licenseID := strconv.Itoa(int(def.id))

	assignedEntitlementID := ent.NewEntitlementID(
		&v2.Resource{Id: &v2.ResourceId{ResourceType: resourceTypeLicense.Id, Resource: licenseID}},
		assignedEntitlement,
	)

	licenseOpts := []resource.LicenseProfileTraitOption{
		resource.WithLicenseName(def.name),
		resource.WithLicenseEntitlementIDs(assignedEntitlementID),
	}

	if def.id == zoom.LicensedUser && purchased > 0 {
		licenseOpts = append(licenseOpts, resource.WithLicenseSeats(purchased, consumed))
	}

	return resource.NewResource(
		def.name,
		resourceTypeLicense,
		licenseID,
		resource.WithLicenseProfileTrait(licenseOpts...),
	)
}

// List returns the three static license tiers. The plan_base fetch is
// best-effort: if the billing scope is missing or the call fails, the
// tiers are still emitted, just without seat numbers on Licensed.
func (l *licenseResourceType) List(ctx context.Context, _ *v2.ResourceId, _ resource.SyncOpAttrs) ([]*v2.Resource, *resource.SyncOpResults, error) {
	logger := ctxzap.Extract(ctx)

	var purchased, consumed int64
	usage, resp, err := l.client.GetAccountPlanUsage(ctx)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
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

// Entitlements emits the single "assigned" assignment-purpose entitlement
// per license tier (each user holds exactly one tier).
func (l *licenseResourceType) Entitlements(_ context.Context, res *v2.Resource, _ resource.SyncOpAttrs) ([]*v2.Entitlement, *resource.SyncOpResults, error) {
	opts := []ent.EntitlementOption{
		ent.WithGrantableTo(resourceTypeUser),
		ent.WithDisplayName(fmt.Sprintf("%s license %s", res.DisplayName, assignedEntitlement)),
		ent.WithDescription(fmt.Sprintf("Holds a %s license seat in Zoom", res.DisplayName)),
	}

	en := ent.NewAssignmentEntitlement(res, assignedEntitlement, opts...)
	return []*v2.Entitlement{en}, &resource.SyncOpResults{}, nil
}

// Grants is a no-op: license grants are emitted from userBuilder.Grants.
func (l *licenseResourceType) Grants(_ context.Context, _ *v2.Resource, _ resource.SyncOpAttrs) ([]*v2.Grant, *resource.SyncOpResults, error) {
	return nil, &resource.SyncOpResults{}, nil
}

// Grant assigns a license tier to a user via PATCH /v2/users/{userId}.
// A pre-flight GET resolves the current tier and short-circuits with
// GrantAlreadyExists when the target tier already matches.
func (l *licenseResourceType) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) ([]*v2.Grant, annotations.Annotations, error) {
	if principal.Id.ResourceType != resourceTypeUser.Id {
		return nil, nil, fmt.Errorf("baton-zoom: only users can be granted a license (got %q)", principal.Id.ResourceType)
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

// Revoke downgrades the user to Basic via PATCH (Zoom has no "no license"
// state). Returns GrantAlreadyRevoked without an API call when the user
// no longer holds the tier or when the revoked tier is Basic itself.
func (l *licenseResourceType) Revoke(ctx context.Context, g *v2.Grant) (annotations.Annotations, error) {
	entitlement := g.Entitlement
	principal := g.Principal

	if principal.Id.ResourceType != resourceTypeUser.Id {
		return nil, fmt.Errorf("baton-zoom: only users can have a license revoked (got %q)", principal.Id.ResourceType)
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

func licenseBuilder(client *zoom.Client) *licenseResourceType {
	return &licenseResourceType{
		resourceType: resourceTypeLicense,
		client:       client,
	}
}
