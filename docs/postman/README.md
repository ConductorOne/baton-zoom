# baton-zoom — Postman collection

Postman v2.1 collection + environment to exercise every Zoom REST endpoint that
the `baton-zoom` connector uses. Useful for:

- Manually validating credentials and scopes before running the connector.
- Reproducing a sync or provisioning bug against a real Zoom account.
- Trying out license tier changes (`PATCH /v2/users/{id}`) and watching their
  effect on seat counts (`GET /v2/accounts/me/plans/usage`) without touching
  the connector code.

## Files

| File                                  | Purpose                                                                                                                                                               |
| ------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `baton-zoom.postman_collection.json`  | All requests, grouped by resource type. Auth is set at the collection level so every request inherits the bearer token.                                               |
| `baton-zoom.postman_environment.json` | Placeholders for `accountId`, `clientId`, `clientSecret`, `accessToken`, and the IDs (`userId`, `groupId`, `roleId`, `contactGroupId`) used by per-resource requests. |

## Import order

1. **Postman → Import** the collection JSON.
2. **Postman → Import** the environment JSON.
3. Activate the **baton-zoom** environment in the top-right environment selector.
4. Fill the three credential fields in the environment:
   - `accountId` — your Zoom account ID (from the S2S OAuth app's _App Credentials_ page).
   - `clientId` — the OAuth app's Client ID.
   - `clientSecret` — the OAuth app's Client Secret.

## Auth flow

The collection uses **Server-to-Server OAuth** ([Zoom docs](https://developers.zoom.us/docs/internal-apps/s2s-oauth/)):

1. Open **Auth → Get access token** and click _Send_.
2. The request hits `POST https://zoom.us/oauth/token` with Basic Auth (`clientId:clientSecret`)
   and `grant_type=account_credentials`.
3. The response includes the granted `scope` list (logged to the Postman
   console) and an `access_token`.
4. A test script stores the token in the environment variable `accessToken`.
5. Every other request in the collection uses **Bearer `{{accessToken}}`** via
   collection-level auth, so you can fire any of them after step 1.
6. Tokens expire after **1 hour**. Re-run _Get access token_ when you start
   seeing HTTP 401.

## Scope checklist

If a request returns HTTP 401 with `Invalid access token, does not contain scopes`,
the missing scope is in the error body. The full list this collection exercises:

| Folder         | Granular scopes                                                                                                                                                                                                                   |
| -------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Users          | `user:read:list_users:admin`, `user:read:user:admin`, `user:write:user:admin`, `user:delete:user:admin`                                                                                                                           |
| License tiers  | `user:read:user:admin`, **`user:update:user:admin`**, `billing:read:plan_usage:admin`                                                                                                                                             |
| Groups         | `group:read:list_groups:admin`, `group:read:list_members:admin`, `group:read:administrator:admin`, `group:write:member:admin`, `group:delete:member:admin`, `group:write:administrator:admin`, `group:delete:administrator:admin` |
| Roles          | `role:read:list_roles:admin`, `role:read:list_members:admin`, `role:write:member:admin`, `role:delete:member:admin`                                                                                                               |
| Contact groups | `contact_group:read:list_groups:admin`, `contact_group:read:list_members:admin`                                                                                                                                                   |
| Invites        | `user:read:list_users:admin`                                                                                                                                                                                                      |

> **Important — the `user:write:user:admin` vs `user:update:user:admin` gotcha**
>
> Zoom split the legacy `user:write:admin` classic scope into three distinct
> granular scopes. `user:write:user:admin` only authorizes `POST /v2/users`
> (create). `PATCH /v2/users/{id}` — the call the connector uses for license
> tier changes — requires the separate **`user:update:user:admin`** scope, and
> fails with Zoom error code 4711 otherwise. Don't assume the "write" scope
> covers updates.

## Folder → connector mapping

| Collection folder | Connector resource type    | Connector files                 |
| ----------------- | -------------------------- | ------------------------------- |
| Users             | `user`                     | `pkg/connector/user.go`         |
| License tiers     | `license` (NEW — CXH-1571) | `pkg/connector/license.go`      |
| Groups            | `group`                    | `pkg/connector/group.go`        |
| Roles             | `role`                     | `pkg/connector/role.go`         |
| Contact groups    | `contactGroup`             | `pkg/connector/contactGroup.go` |
| Invites           | `invite`                   | `pkg/connector/invite.go`       |

## Reproducing the license roundtrip end-to-end

This sequence mirrors the CI workflow's license job:

1. **Auth → Get access token** (token cached in env).
2. **License tiers → Get plan usage (seat counts)** — note `plan_base.usage`.
3. Set `userId` in the environment to a Basic test user's ID.
4. **License tiers → Grant Licensed tier (PATCH type=2)** — should return 204.
5. **License tiers → Get plan usage** again — `usage` should be +1.
6. **License tiers → Revoke / downgrade to Basic (PATCH type=1)** — should return 204.
7. **License tiers → Get plan usage** — `usage` should be back to the original.

If any of the PATCH calls return 401 with the `user:update:user:admin` scope
error, see the gotcha box above.
