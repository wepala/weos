---
title: MCP Tools
parent: Reference
layout: default
nav_order: 4
---

# MCP Tools

The WeOS MCP server exposes tools organized into four service groups. All tools use JSON Schema for input validation and return structured JSON output.

**Server details:**
- Name: `weos`
- Title: `WeOS MCP Server`
- Version: `0.1.0`
- Transport: stdio

## Person Tools

Service name: `person`

### `person_create`

Create a new person.

**Input:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `given_name` | string | Yes | First name |
| `family_name` | string | Yes | Last name |
| `email` | string | Yes | Email address |

**Output:** PersonOutput (id, given_name, family_name, name, email, status, created_at)

### `person_get`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Person URN |

### `person_list`

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `cursor` | string | No | | Pagination cursor |
| `limit` | int | No | 20 | Max items (1-100) |

**Output:** `{data: PersonOutput[], cursor: string, has_more: bool}`

### `person_update`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Person URN |
| `given_name` | string | Yes | First name |
| `family_name` | string | Yes | Last name |
| `email` | string | No | Email address |
| `avatar_url` | string | No | Avatar image URL |
| `status` | string | No | `"active"` or `"archived"` |

### `person_delete`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Person URN |

**Output:** `{success: bool}`

---

## Organization Tools

Service name: `organization`

### `organization_create`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Display name |
| `slug` | string | Yes | URL-safe identifier |

**Output:** OrganizationOutput (id, name, slug, description, url, logo_url, status, created_at)

### `organization_get`

| Field | Type | Required |
|-------|------|----------|
| `id` | string | Yes |

### `organization_list`

| Field | Type | Required | Default |
|-------|------|----------|---------|
| `cursor` | string | No | |
| `limit` | int | No | 20 |

### `organization_update`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Organization URN |
| `name` | string | Yes | Display name |
| `slug` | string | No | URL slug |
| `description` | string | No | Description |
| `url` | string | No | Website URL |
| `logo_url` | string | No | Logo image URL |
| `status` | string | No | `"active"` or `"archived"` |

### `organization_delete`

| Field | Type | Required |
|-------|------|----------|
| `id` | string | Yes |

---

## Resource Type Tools

Service name: `resource-type`

### `resource_type_create`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Display name |
| `slug` | string | Yes | URL-safe identifier |
| `description` | string | No | Description |
| `context` | object | No | JSON-LD context |
| `schema` | object | No | JSON Schema for validation |

**Output:** ResourceTypeOutput (id, name, slug, description, context, schema, status, created_at)

### `resource_type_get`

| Field | Type | Required |
|-------|------|----------|
| `id` | string | Yes |

### `resource_type_list`

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `cursor` | string | No | | Pagination cursor |
| `limit` | int | No | 20 | Max items (1-100) |
| `includeAll` | bool | No | false | Include value objects and abstract types |

### `resource_type_update`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Resource type URN |
| `name` | string | Yes | Display name |
| `slug` | string | No | URL slug |
| `description` | string | No | Description |
| `context` | object | No | JSON-LD context |
| `schema` | object | No | JSON Schema |
| `status` | string | No | `"active"` or `"archived"` |

### `resource_type_delete`

| Field | Type | Required |
|-------|------|----------|
| `id` | string | Yes |

### `resource_type_preset_list`

No input required.

**Output:** `{presets: [{name, description, types: [slug]}]}`

### `resource_type_preset_install`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Preset name (core, auth, website, events, tasks, knowledge, ecommerce) |
| `update` | bool | No | Update existing types instead of skipping |

**Output:** `{created: [slug], updated: [slug], skipped: [slug]}`

---

## Resource Tools

Service name: `resource`

### `resource_create`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type_slug` | string | Yes | Resource type slug |
| `data` | object | Yes | Resource data (JSON matching the type's schema) |

**Output:** ResourceOutput (id, type_slug, data, status, created_at)

### `resource_get`

| Field | Type | Required |
|-------|------|----------|
| `id` | string | Yes |

### `resource_list`

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `type_slug` | string | Yes | | Resource type slug |
| `cursor` | string | No | | Pagination cursor |
| `limit` | int | No | 20 | Max items (1-100) |
| `sort_by` | string | No | | Column name to sort by |
| `sort_order` | string | No | | `"asc"` or `"desc"` |

### `resource_update`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Resource URN |
| `data` | object | Yes | Updated resource data |

### `resource_delete`

| Field | Type | Required |
|-------|------|----------|
| `id` | string | Yes |

---

## Pagination

All list operations use cursor-based pagination:

```json
{
  "data": [...],
  "cursor": "eyJpZCI6Imxhc3QtaWQifQ==",
  "has_more": true
}
```

Pass the `cursor` value from the response as the `cursor` parameter in the next request to get the next page. When `has_more` is `false`, there are no more results.
