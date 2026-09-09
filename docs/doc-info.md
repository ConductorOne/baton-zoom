# `baton-zoom` doc-info

While developing the connector, please fill out this form. This information is needed to write docs and to help other users set up the connector.

## Connector capabilities

### 1. What resources does the connector sync?

| Resource           | Trait                   | Notes                                                                                                                                                                                                                                        |
| ------------------ | ----------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Users**          | `TRAIT_USER`            | `GET /v2/users`. Inactive users included when `--sync-inactive-users` is set. The existing `type` profile field is preserved; `GET /v2/users/{userId}` supplies `group_ids`, `role_id`, and `type` for grant emission.                     |
| **Invites**        | `TRAIT_USER`            | Pending users (`status=pending`). No native ID yet — synced as a separate type.                                                                                                                                                              |
| **Groups**         | `TRAIT_GROUP`           | `GET /v2/groups`. Member grants come from each user's `group_ids`. Admin grants come from `GET /v2/groups/{groupId}/admins`. Both entitlements remain provisionable.                                                                         |
| **Contact Groups** | `TRAIT_GROUP`           | Read-only. Membership includes both users and nested user-groups (`GET /v2/contacts/groups/{id}/members`).                                                                                                                                   |
| **Roles**          | `TRAIT_ROLE`            | `GET /v2/roles`. A user holds one `role_id`. Membership grants are emitted from the user side; role-side `Grants()` is a no-op (`SkipGrants`). Assign/unassign still uses `POST`/`DELETE /v2/roles/{roleId}/members`.                        |
| **Licenses**       | `TRAIT_LICENSE_PROFILE` | Static tiers Basic (`1`), Licensed (`2`), Unassigned (`4`). Grants come from the user's `type`. Seat counts on Licensed require `billing:read:plan_usage:admin`.                                                                             |

### 2. Can the connector provision any resources? If so, which ones?

Yes:

| Resource     | Operations                                         | API surface                                                                                             |
| ------------ | -------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| **Users**    | **Create account**, **Delete user**, **`transfer_and_delete_user` action** | `POST /v2/users` (action=`create`); `DELETE /v2/users/{userId}` (optional `transfer_email` / `transfer_meeting` / `transfer_webinar` / `transfer_recording`) |
| **Groups**   | Add/Remove members, Add/Remove admins              | `POST /v2/groups/{groupId}/members`; `DELETE /v2/groups/{groupId}/members/{userId}`; same for `/admins` |
| **Roles**    | Assign/Unassign role to a user                     | `POST /v2/roles/{roleId}/members`; `DELETE /v2/roles/{roleId}/members/{userId}`                         |
| **Licenses** | Grant (assign a tier), Revoke (downgrade to Basic) | `PATCH /v2/users/{userId}` with body `{"type": N}`                                                      |

**License Revoke semantics:** Zoom has no "no license" state — Basic is the floor and does not consume a seat. Revoking the **Licensed** or **Unassigned** tier downgrades the user back to Basic, freeing the seat. Revoking a **Basic** grant is a logical no-op (returns `GrantAlreadyRevoked` without an API call).

Contact Groups are intentionally **read-only** (no membership write endpoints are exposed by Zoom's public API).

### Grant emission

Group, role and license memberships are emitted from the **principal** side:
`GET /v2/users/{userId}` returns `group_ids`, `role_id` and `type` together, so
one lookup per user covers all three and the group and role builders never
rescan every member to invert the relationship. There is no user-side field for
group **admin** status.

| Grant | Emitted from | Zoom source | Sync filter |
| ----- | ------------ | ----------- | ----------- |
| Group `member` | `userBuilder.Grants` | `GET /v2/users/{userId}` → `group_ids` | Emit only when the `group` resource type is selected |
| Group `admin` | `groupBuilder.Grants` | `GET /v2/groups/{groupId}/admins` (paginated) | Group builder only runs when groups are synced |
| Role `member` | `userBuilder.Grants` | `GET /v2/users/{userId}` → `role_id` | Emit only when the `role` resource type is selected |
| License `assigned` | `userBuilder.Grants` | `GET /v2/users/{userId}` → `type` | Emit only when the `license` resource type is selected |
| Contact group `member` | `contactGroupBuilder.Grants` | `GET /v2/contacts/groups/{id}/members` | Emit user principals only when `user` is selected and nested-group principals only when `group` is selected |

Empty or nil `--sync-resource-types` means sync everything (same as before). An explicit filter must include the **target** type or the corresponding grants are omitted. When none of `group`, `role` or `license` is selected, `userBuilder.Grants` skips the per-user lookup altogether.

`group:read:list_members:admin` and `role:read:list_members:admin` are not required. Provisioning still uses the member write/delete scopes.

Official API references:

- [List users](https://developers.zoom.us/docs/api/users/#tag/users/GET/users)
- [Get a user](https://developers.zoom.us/docs/api/users/#tag/users/GET/users/{userId})
- [List groups](https://developers.zoom.us/docs/api/groups/)
- [List group admins](https://developers.zoom.us/docs/api/groups/#tag/groups/GET/groups/{groupId}/admins)
- [List roles](https://developers.zoom.us/docs/api/roles/)

## Connector credentials

### 1. What credentials or information are needed to set up the connector?

The connector uses **Server-to-Server OAuth (S2S OAuth)**. The user must provide three values:

| Field         | Env var                    | Required |
| ------------- | -------------------------- | -------- |
| Account ID    | `BATON_ACCOUNT_ID`         | Yes      |
| Client ID     | `BATON_ZOOM_CLIENT_ID`     | Yes      |
| Client Secret | `BATON_ZOOM_CLIENT_SECRET` | Yes      |

The connector exchanges these for a short-lived bearer token at `POST https://zoom.us/oauth/token?grant_type=account_credentials&account_id=<account_id>` using HTTP Basic auth (`client_id:client_secret`). The token expires after ~1 hour and is requested at startup.

### 2. For each item above

#### How does a user create or look up that credential?

All three credentials originate from the same Server-to-Server OAuth app in the [Zoom App Marketplace](https://marketplace.zoom.us/).

1. Go to the [Zoom App Marketplace](https://marketplace.zoom.us/) and sign in as a user with **Account Owner** (or Admin with developer privileges) status.
2. Click **Develop** → **Build App**.
3. Locate the **Server-to-Server OAuth** card and click **Create**.
4. Name the app (e.g. "ConductorOne") and click **Create**.
5. The **App Credentials** page opens. The **Account ID**, **Client ID**, and **Client Secret** are all shown on this page. Copy and save them.
6. Click **Continue**, fill out **Basic information** (company name, contact, etc.), click **Continue**.
7. Skip the **Feature** page (no changes needed), click **Continue**.
8. On the **Scopes** page, click **+ Add Scopes** and add the scopes listed below (Sync, and optionally Provisioning and Billing).
9. Click **Continue**, then **Activate your app**.

Reference: <https://developers.zoom.us/docs/internal-apps/create/>

#### Does the credential need any specific scopes or permissions? If so, list them here.

Yes. All scopes are **granular admin scopes**. The connector validates each scope only against the resource type that needs it — missing optional scopes degrade individual resource types gracefully (e.g. without `billing:read:plan_usage:admin`, License resources are still synced but seat counts are omitted).

#### Sync-only (read) scopes

Minimum set required for read-only sync of all resource types:

```text
contact_group:read:list_groups:admin
contact_group:read:list_members:admin
group:read:list_groups:admin
group:read:administrator:admin
role:read:list_roles:admin
user:read:user:admin
user:read:list_users:admin
```

Optional (recommended) for license seat reporting:

```text
billing:read:plan_usage:admin
```

> Without `billing:read:plan_usage:admin`, the `Licenses` resource type still syncs — it just omits the `purchased_seats` / `consumed_seats` fields on the Licensed tier. The connector logs at debug and continues.

#### Provisioning (read + write) scopes

Add **all sync scopes above**, plus the write scopes below. `user:delete:user:admin` is also required to invoke the `transfer_and_delete_user` connector action (`DELETE /v2/users/{userId}`, with optional transfer query parameters). Recipient lookup on that action reuses `user:read:user:admin` from the sync set.

```text
user:write:user:admin
user:update:user:admin
user:delete:user:admin
role:write:member:admin
role:delete:member:admin
group:write:member:admin
group:delete:member:admin
group:write:administrator:admin
group:delete:administrator:admin
```

> **Why both `user:write` and `user:update`?** Zoom splits writes into distinct granular scopes: `user:write:user:admin` only authorizes `POST /v2/users` (account creation), and `user:update:user:admin` is required for `PATCH /v2/users/{userId}` (license tier changes). Requesting only `user:write` is a common pitfall — license grant/revoke will fail with Zoom error code `4711: Invalid access token, does not contain scopes`.

Per-resource breakdown of which scope unlocks which operation:

| Resource | Operation                          | Scope                                                                  |
| -------- | ---------------------------------- | ---------------------------------------------------------------------- |
| User     | CreateAccount                      | `user:write:user:admin`                                                |
| User     | Delete                             | `user:delete:user:admin`                                               |
| User     | `transfer_and_delete_user`         | `user:delete:user:admin`; `user:read:user:admin` when verifying `transfer_email` |
| Group    | Add/remove member                  | `group:write:member:admin` + `group:delete:member:admin`               |
| Group    | Add/remove admin                   | `group:write:administrator:admin` + `group:delete:administrator:admin` |
| Role     | Assign/unassign role               | `role:write:member:admin` + `role:delete:member:admin`                 |
| License  | Grant / Revoke (PATCH user `type`) | `user:update:user:admin` (distinct from `user:write:user:admin`)       |

#### Difference between sync and provisioning scopes

| Scope group                        | Sync only | Sync + Provision           |
| ---------------------------------- | --------- | -------------------------- |
| Read scopes (`*:read:*:admin`)     | Yes       | Yes                        |
| `billing:read:plan_usage:admin`    | Optional  | Optional                   |
| `user:write:user:admin`            | No        | Yes                        |
| `user:update:user:admin`           | No        | Yes (license Grant/Revoke) |
| `user:delete:user:admin`           | No        | Yes (user delete + `transfer_and_delete_user`) |
| `role:write:member:admin`          | No        | Yes                        |
| `role:delete:member:admin`         | No        | Yes                        |
| `group:write:member:admin`         | No        | Yes                        |
| `group:delete:member:admin`        | No        | Yes                        |
| `group:write:administrator:admin`  | No        | Yes                        |
| `group:delete:administrator:admin` | No        | Yes                        |

#### What level of access does the user need to create the credentials?

- **Zoom Account role:** Must be **Account Owner**, or an Admin with developer access enabled by the Account Owner. The S2S OAuth flow plus the admin scopes (`*:admin`) listed above require ownership/admin privileges. A regular Member account cannot create or activate a Server-to-Server OAuth app with these scopes.
- **Zoom plan:** Requires a paid plan — **Pro, Business, Business Plus, or Enterprise**. The Free (Basic) plan does not support Server-to-Server OAuth apps with admin scopes and does not expose `/accounts/me/plans/usage`.
- **Marketplace access:** The user must be able to access <https://marketplace.zoom.us/> with their Zoom credentials and reach the **Develop** menu (only owners/admins see it).

After activation, the credentials remain valid until the app is deactivated or its secret is rotated from the **App Credentials** page. Rotation requires the same Account Owner / Admin role.
