---
title: "Auth: Roles and Access"
parent: Tutorials
layout: default
nav_order: 3
---

# Auth: Roles and Access

WeOS uses a hybrid RBAC (Role-Based Access Control) + ownership model to control who can do what. In this tutorial you'll create roles, assign them to users, and configure which resource types each role can access.

## Prerequisites

- WeOS built and running (see [Running WeOS]({% link _tutorials/running-weos.md %}))
- The `tasks` preset installed (`./bin/weos resource-type preset install tasks`)

## How Auth Works in WeOS

The **auth** preset (auto-installed on first run) provides three resource types:
- **User** — a person who can log in
- **Role** — a named permission set (e.g., "Editor", "Viewer")
- **Account** — an organizational tenant

Access control is enforced at two levels:
1. **Role-based policies** — which resource types a role can read, modify, or delete
2. **Resource-level permissions** — per-resource grants for specific users (ownership model)

Actions use ODRL IRIs:
- `http://www.w3.org/ns/odrl/2/read` — view resources
- `http://www.w3.org/ns/odrl/2/modify` — create and update resources
- `http://www.w3.org/ns/odrl/2/delete` — delete resources

## Step 1: Seed Users and Roles

The quickest way to set up auth is to run the seed command:

```bash
make dev-seed
```

This creates:
- An **admin** user (`admin@weos.dev`) with the "admin" role
- A **member** user (`member@weos.dev`) with the "member" role
- Default Casbin authorization policies

If you prefer to create users manually, use the API:

```bash
# Create a user via the API
curl -X POST http://localhost:8080/api/persons \
  -H "Content-Type: application/json" \
  -d '{"given_name": "Jane", "family_name": "Editor", "email": "jane@example.com"}'
```

## Step 2: Configure Role Access via the API

Manage which roles can access which resource types using the role-access settings endpoint:

```bash
# Get current role-access configuration
curl http://localhost:8080/api/settings/role-access
```

The response shows a map of roles to their permitted actions per resource type:

```json
{
  "roles": {
    "admin": {
      "project": [
        "http://www.w3.org/ns/odrl/2/read",
        "http://www.w3.org/ns/odrl/2/modify",
        "http://www.w3.org/ns/odrl/2/delete"
      ],
      "task": [
        "http://www.w3.org/ns/odrl/2/read",
        "http://www.w3.org/ns/odrl/2/modify",
        "http://www.w3.org/ns/odrl/2/delete"
      ]
    },
    "editor": {
      "project": [
        "http://www.w3.org/ns/odrl/2/read",
        "http://www.w3.org/ns/odrl/2/modify"
      ],
      "task": [
        "http://www.w3.org/ns/odrl/2/read",
        "http://www.w3.org/ns/odrl/2/modify"
      ]
    }
  }
}
```

Update role access:

```bash
curl -X PUT http://localhost:8080/api/settings/role-access \
  -H "Content-Type: application/json" \
  -d '{
    "roles": {
      "admin": {
        "project": ["http://www.w3.org/ns/odrl/2/read", "http://www.w3.org/ns/odrl/2/modify", "http://www.w3.org/ns/odrl/2/delete"],
        "task": ["http://www.w3.org/ns/odrl/2/read", "http://www.w3.org/ns/odrl/2/modify", "http://www.w3.org/ns/odrl/2/delete"]
      },
      "viewer": {
        "project": ["http://www.w3.org/ns/odrl/2/read"],
        "task": ["http://www.w3.org/ns/odrl/2/read"]
      }
    }
  }'
```

## Step 3: Manage Roles

Configure which roles exist in the system:

```bash
# Get current roles
curl http://localhost:8080/api/settings/roles

# Update roles list
curl -X PUT http://localhost:8080/api/settings/roles \
  -H "Content-Type: application/json" \
  -d '{"roles": ["admin", "editor", "viewer"]}'
```

## Step 4: Manage Users

Assign roles to users via the user management API:

```bash
# List users
curl http://localhost:8080/api/users

# Update a user's role (replace USER_ID)
curl -X PUT http://localhost:8080/api/users/USER_ID \
  -H "Content-Type: application/json" \
  -d '{"role": "editor"}'
```

## Step 5: Resource-Level Permissions

For fine-grained control, grant permissions on individual resources:

```bash
# Grant read access to a specific project for a user
curl -X POST http://localhost:8080/api/resources/PROJECT_ID/permissions \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "USER_AGENT_ID",
    "actions": ["http://www.w3.org/ns/odrl/2/read"]
  }'

# List permissions on a resource
curl http://localhost:8080/api/resources/PROJECT_ID/permissions

# Revoke a user's permissions
curl -X DELETE http://localhost:8080/api/resources/PROJECT_ID/permissions/USER_AGENT_ID
```

## Step 6: Test Access Control

In development mode (no OAuth), use the `X-Dev-Agent` header to simulate different users:

```bash
# As admin — should succeed
curl -H "X-Dev-Agent: admin@weos.dev" \
  http://localhost:8080/api/project

# As a different user
curl -H "X-Dev-Agent: viewer@example.com" \
  http://localhost:8080/api/project
```

{: .note }
> **RBAC enforcement requires OAuth.** In development mode (no OAuth), the authorization middleware is not applied — all users can read and write. To test role-based access control, enable OAuth by setting `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`. With OAuth enabled, the authorization middleware enforces Casbin policies, and a viewer role attempting a write operation will receive a 403 Forbidden response.

## How the Middleware Works

The authorization flow for API requests:

1. **SoftAuth middleware** (dev mode) or **RequireAuth** (production) identifies the user
2. **Impersonation middleware** swaps identity if an admin is impersonating
3. **AuthorizeResource middleware** checks Casbin policies:
   - Maps HTTP method to ODRL action (GET → read, POST/PUT → modify, DELETE → delete)
   - Checks if the user's role has the action for the resource type
   - Roles with no configured policies get read-only access by default

## OAuth in Production

For production use, configure Google OAuth:

```bash
export GOOGLE_CLIENT_ID="your-client-id"
export GOOGLE_CLIENT_SECRET="your-client-secret"
export FRONTEND_URL="https://your-site.com"
```

When OAuth is enabled:
- `/api/auth/login` redirects to Google sign-in
- `/api/auth/callback` handles the OAuth callback
- `/api/auth/me` returns the authenticated user's profile
- `/api/auth/logout` ends the session
- All protected routes require a valid session

## What You've Learned

- How WeOS combines RBAC with per-resource permissions
- How to create and manage roles
- How to configure role-based access for resource types
- How to grant resource-level permissions
- How authorization middleware enforces access control
- How to test with different user identities in development

## What's Next

- [Configuration]({% link _reference/configuration.md %}) — all OAuth and session settings
- [API Endpoints]({% link _reference/api-endpoints.md %}) — full auth API reference
