# baton-zoom

`baton-zoom` is a connector for Zoom built using the [Baton SDK](https://github.com/conductorone/baton-sdk). It communicates with the Zoom API to sync users, groups, roles, contact groups, invites, and license tiers.

Check out [Baton](https://github.com/conductorone/baton) to learn more the project in general.

# Getting Started

## Prerequisites

1. Zoom [server to server app](https://developers.zoom.us/docs/internal-apps/create/) created in [marketplace](https://marketplace.zoom.us/)
2. Scopes for syncing only(no provisioning):

- contact_group:read:list_groups:admin
- contact_group:read:list_members:admin
- group:read:list_groups:admin
- group:read:administrator:admin
- role:read:list_roles:admin
- user:read:user:admin
- user:read:list_users:admin
- billing:read:plan_usage:admin (optional, used to surface purchased vs. consumed Licensed seat counts)

Scopes for provisioning (grant/revoke) and the `transfer_and_delete_user` action

- role:write:member:admin
- role:delete:member:admin
- group:write:member:admin
- group:delete:member:admin
- group:write:administrator:admin
- group:delete:administrator:admin
- user:write:user:admin (create users)
- user:update:user:admin (assign/revoke license tier via PATCH /v2/users/{userId})
- user:delete:user:admin (delete users; also required for the transfer_and_delete_user action)

3. Pro or higher [plan](https://zoom.us/pricing)
4. Activate the App for Account ID, Client ID and Client Secret needed to use the API

## License tiers

The connector models Zoom's three user license tiers as a `license` resource type:

| Tier       | Zoom `type` | Consumes a seat?         |
| ---------- | ----------- | ------------------------ |
| Basic      | `1`         | No                       |
| Licensed   | `2`         | Yes                      |
| Unassigned | `4`         | No (no meetings license) |

Granting a license PATCHes the user's `type` field to the target tier. Revoking a license is a downgrade to Basic — Zoom has no "no license" state, and Basic is the floor. Revoking a Basic grant is a no-op since Basic does not occupy a seat.

When the `billing:read:plan_usage:admin` scope is granted, the Licensed resource is decorated with `purchased_seats` and `consumed_seats` (from `GET /v2/accounts/me/plans/usage` → `plan_base.hosts` / `plan_base.usage`). Without the scope, sync still succeeds — only the seat counts are omitted.

## brew

```
brew install conductorone/baton/baton conductorone/baton/baton-zoom
baton-zoom
baton resources
```

## docker

```
docker run --rm -v "$(pwd):/out" -e BATON_ZOOM_CLIENT_ID=clientId -e BATON_ZOOM_CLIENT_SECRET=clientSecret -e BATON_ACCOUNT_ID=accountId public.ecr.aws/conductorone/baton-zoom:latest -f "/out/sync.c1z"
docker run --rm -v $(pwd):/out ghcr.io/conductorone/baton:latest -f "/out/sync.c1z" resources
```

## source

```
go install github.com/conductorone/baton/cmd/baton@main
go install github.com/conductorone/baton-zoom/cmd/baton-zoom@main

BATON_ZOOM_CLIENT_ID=clientId BATON_ZOOM_CLIENT_SECRET=clientSecret BATON_ACCOUNT_ID=accountId
baton resources
```

# Data Model

`baton-zoom` syncs the following Zoom resources:

- **Users** — `GET /v2/users`, plus `GET /v2/users/{userId}` during grant emission. The C1 profile retains the Zoom license `type`; group membership, role assignment, and license grants come from `group_ids`, `role_id`, and `type` returned by the per-user request.
- **Invites** — pending users (`GET /v2/users?status=pending`).
- **Groups** — `GET /v2/groups`. Member grants come from user `group_ids`. Admin grants come from `GET /v2/groups/{groupId}/admins`.
- **Contact Groups** — `GET /v2/contacts/groups` and `GET /v2/contacts/groups/{id}/members` (read-only).
- **Roles** — `GET /v2/roles`. Membership grants come from each user's `role_id`.
- **Licenses** — static Basic / Licensed / Unassigned tiers from `User.type`. Seat counts use `GET /v2/accounts/me/plans/usage` when the billing scope is present.

Grant emission for groups, roles, and licenses is skipped when that resource type is excluded from `--sync-resource-types`. See [`docs/doc-info.md`](docs/doc-info.md) for endpoints and filter semantics. Customer setup: [`docs/connector.mdx`](docs/connector.mdx).

# Contributing, Support, and Issues

We started Baton because we were tired of taking screenshots and manually building spreadsheets. We welcome contributions, and ideas, no matter how small -- our goal is to make identity and permissions sprawl less painful for everyone. If you have questions, problems, or ideas: Please open a Github Issue!

See [CONTRIBUTING.md](https://github.com/ConductorOne/baton/blob/main/CONTRIBUTING.md) for more details.

# `baton-zoom` Command Line Usage

```
baton-zoom

Usage:
  baton-zoom [flags]
  baton-zoom [command]

Available Commands:
  capabilities       Get connector capabilities
  completion         Generate the autocompletion script for the specified shell
  help               Help about any command

Flags:
      --account-id string           required: Account ID used to generate token providing access to Zoom API. ($BATON_ACCOUNT_ID)
      --client-id string            The client ID used to authenticate with ConductorOne ($BATON_CLIENT_ID)
      --client-secret string        The client secret used to authenticate with ConductorOne ($BATON_CLIENT_SECRET)
  -f, --file string                 The path to the c1z file to sync with ($BATON_FILE) (default "sync.c1z")
  -h, --help                        help for baton-zoom
      --log-format string           The output format for logs: json, console ($BATON_LOG_FORMAT) (default "json")
      --log-level string            The log level: debug, info, warn, error ($BATON_LOG_LEVEL) (default "info")
  -p, --provisioning                This must be set in order for provisioning actions to be enabled ($BATON_PROVISIONING)
      --skip-full-sync              This must be set to skip a full sync ($BATON_SKIP_FULL_SYNC)
      --sync-inactive-users         Sync inactive Zoom users alongside active users ($BATON_SYNC_INACTIVE_USERS)
      --ticketing                   This must be set to enable ticketing support ($BATON_TICKETING)
  -v, --version                     version for baton-zoom
      --zoom-client-id string       required: Client ID used to generate token providing access to Zoom API. ($BATON_ZOOM_CLIENT_ID)
      --zoom-client-secret string   required: Client Secret used to generate token providing access to Zoom API. ($BATON_ZOOM_CLIENT_SECRET)

Use "baton-zoom [command] --help" for more information about a command.
```
