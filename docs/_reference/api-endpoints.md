---
title: API Endpoints
parent: Reference
layout: default
nav_order: 3
---

# API Endpoints

All API routes are prefixed with `/api`. Non-API paths serve the embedded SPA frontend.

## Public Endpoints

### Health

| Method | Path | Response |
|--------|------|----------|
| GET | `/api/health` | `{"status": "ok"}` |

### Authentication (OAuth mode)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/auth/login` | Redirect to Google sign-in |
| GET | `/api/auth/callback` | OAuth callback handler |
| GET | `/api/auth/me` | Current user profile |
| POST | `/api/auth/logout` | End session |

In development mode (no OAuth), `/api/auth/me` returns a dev user based on the `X-Dev-Agent` header (default: `admin@weos.dev`).

## Resource Types

| Method | Path | Description | Request Body |
|--------|------|-------------|-------------|
| POST | `/api/resource-types` | Create a resource type | `{name, slug, context?, description?}` |
| GET | `/api/resource-types` | List resource types | Query: `cursor`, `limit` (default 20), `includeAll` |
| GET | `/api/resource-types/:id` | Get a resource type | |
| PUT | `/api/resource-types/:id` | Update a resource type | `{name?, slug?, description?, context?, schema?, status?}` |
| DELETE | `/api/resource-types/:id` | Delete a resource type | |

**Response format:**
```json
{
  "id": "urn:type:blog-post",
  "name": "Blog Post",
  "slug": "blog-post",
  "description": "A blog post entry",
  "context": {"@vocab": "https://schema.org/", "@type": "BlogPosting"},
  "schema": {"type": "object", "properties": {...}},
  "status": "active",
  "created_at": "2026-04-05T12:00:00Z"
}
```

## Resource Type Presets

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/resource-types/presets` | List available presets |
| POST | `/api/resource-types/presets/:name` | Install a preset (query: `update=true` to sync existing types) |

**Install response:**
```json
{
  "created": ["menu", "menu-item"],
  "updated": [],
  "skipped": []
}
```

## Dynamic Resources

Resources are accessed under `/api` with their type slug:

| Method | Path | Description | Request Body |
|--------|------|-------------|-------------|
| POST | `/api/:typeSlug` | Create a resource | JSON data matching the type's schema |
| GET | `/api/:typeSlug` | List resources | Query: `cursor`, `limit`, `sort_by`, `sort_order`, `_filter[field][op]=value` |
| GET | `/api/:typeSlug/:id` | Get a resource | |
| PUT | `/api/:typeSlug/:id` | Update a resource | JSON data |
| DELETE | `/api/:typeSlug/:id` | Delete a resource | |

**Query parameters for list:**

| Parameter | Description | Example |
|-----------|-------------|---------|
| `cursor` | Pagination cursor from previous response | |
| `limit` | Items per page (default: 20) | `?limit=50` |
| `sort_by` | Column to sort by | `?sort_by=created_at` |
| `sort_order` | `asc` or `desc` | `?sort_order=desc` |
| `_filter[field][op]` | Filter by field with operator | `?_filter[status][eq]=active` |
| `filter_field` + `filter_value` | Simple field filter | `?filter_field=status&filter_value=active` |

**Response format:**
```json
{
  "id": "urn:task:abc123",
  "type_slug": "task",
  "data": {"@graph": [...]},
  "status": "active",
  "created_at": "2026-04-05T12:00:00Z"
}
```

**List response:**
```json
{
  "data": [...],
  "cursor": "next-page-token",
  "has_more": true
}
```

Supports `Accept: application/ld+json` header for JSON-LD responses.

## Persons

| Method | Path | Description | Request Body |
|--------|------|-------------|-------------|
| POST | `/api/persons` | Create a person | `{given_name, family_name, email?}` |
| GET | `/api/persons` | List persons | Query: `cursor`, `limit` |
| GET | `/api/persons/:id` | Get a person | |
| PUT | `/api/persons/:id` | Update a person | `{given_name?, family_name?, email?, avatar_url?, status?}` |
| DELETE | `/api/persons/:id` | Delete a person | |

**Response format:**
```json
{
  "id": "urn:person:abc123",
  "given_name": "Jane",
  "family_name": "Smith",
  "name": "Jane Smith",
  "email": "jane@example.com",
  "avatar_url": "",
  "status": "active",
  "created_at": "2026-04-05T12:00:00Z"
}
```

The `name` field is auto-computed from `given_name` + `family_name`.

## Organizations

| Method | Path | Description | Request Body |
|--------|------|-------------|-------------|
| POST | `/api/organizations` | Create an organization | `{name, slug}` |
| GET | `/api/organizations` | List organizations | Query: `cursor`, `limit` |
| GET | `/api/organizations/:id` | Get an organization | |
| GET | `/api/organizations/:id/members` | List organization members | Query: `cursor`, `limit` |
| PUT | `/api/organizations/:id` | Update an organization | `{name?, slug?, description?, url?, logo_url?, status?}` |
| DELETE | `/api/organizations/:id` | Delete an organization | |

## Users (Admin)

| Method | Path | Description | Request Body |
|--------|------|-------------|-------------|
| GET | `/api/users` | List users | |
| GET | `/api/users/:id` | Get a user | |
| PUT | `/api/users/:id` | Update a user (name, role) | `{name?, role?}` |

## Settings

### Sidebar

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/settings/sidebar` | Get sidebar configuration (query: `role` for role-specific view) |
| PUT | `/api/settings/sidebar` | Update sidebar configuration |

**Sidebar format:**
```json
{
  "hidden_slugs": ["web-page-element"],
  "menu_groups": {
    "Content": ["article", "blog-post"],
    "Structure": ["web-site", "web-page"]
  }
}
```

### Roles

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/settings/roles` | Get role list |
| PUT | `/api/settings/roles` | Update role list: `{roles: ["admin", "editor", "viewer"]}` |

### Role Access

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/settings/role-access` | Get role-to-resource access map |
| PUT | `/api/settings/role-access` | Update role-to-resource access map |

## Impersonation (Admin)

| Method | Path | Description | Request Body |
|--------|------|-------------|-------------|
| POST | `/api/admin/impersonate` | Start impersonating a user | `{agent_id}` |
| POST | `/api/admin/stop-impersonation` | Stop impersonating | |
| GET | `/api/admin/impersonation-status` | Check impersonation status | |

## Resource Permissions

| Method | Path | Description | Request Body |
|--------|------|-------------|-------------|
| POST | `/api/:typeSlug/:id/permissions` | Grant permissions | `{agent_id, actions[]}` |
| GET | `/api/:typeSlug/:id/permissions` | List permissions | |
| DELETE | `/api/:typeSlug/:id/permissions/:agentId` | Revoke permissions | |

## Error Responses

| Status | Meaning |
|--------|---------|
| 400 | Bad request (validation error) |
| 401 | Not authenticated |
| 403 | Not authorized (insufficient role/permissions) |
| 404 | Resource not found |
| 500 | Internal server error |

Error response format:
```json
{
  "message": "resource not found"
}
```
