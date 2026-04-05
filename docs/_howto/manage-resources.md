---
title: Manage Resources
parent: How-to Guides
layout: default
nav_order: 4
---

# Manage Resources

Resources are instances of resource types. Manage them via CLI, API, or MCP.

## Create

### CLI

```bash
weos resource create --type blog-post \
  --data '{"headline": "Hello World", "articleBody": "Welcome!", "author": "Jane"}'
```

### API

```bash
curl -X POST http://localhost:8080/blog-post \
  -H "Content-Type: application/json" \
  -d '{"headline": "Hello World", "articleBody": "Welcome!", "author": "Jane"}'
```

Note: the API route uses the type slug directly (`/blog-post`), not `/api/resources/`.

## List

### CLI

```bash
weos resource list --type blog-post
weos resource list --type blog-post --limit 50
weos resource list --type blog-post --cursor "next-page-token"
```

### API

```bash
# Basic list
curl http://localhost:8080/blog-post

# With pagination and sorting
curl "http://localhost:8080/blog-post?limit=10&sort_by=created_at&sort_order=desc"

# With filtering
curl "http://localhost:8080/blog-post?_filter[status][eq]=active"
curl "http://localhost:8080/blog-post?filter_field=author&filter_value=Jane"
```

## Get

### CLI

```bash
weos resource get urn:blog-post:abc123
```

### API

```bash
curl http://localhost:8080/blog-post/urn:blog-post:abc123

# JSON-LD format
curl -H "Accept: application/ld+json" http://localhost:8080/blog-post/urn:blog-post:abc123
```

## Update

### API

```bash
curl -X PUT http://localhost:8080/blog-post/urn:blog-post:abc123 \
  -H "Content-Type: application/json" \
  -d '{"headline": "Updated Title", "articleBody": "New content"}'
```

## Delete

### CLI

```bash
weos resource delete urn:blog-post:abc123
```

### API

```bash
curl -X DELETE http://localhost:8080/blog-post/urn:blog-post:abc123
```

Deletion is a soft delete — the resource is marked as "archived" via a `Resource.Deleted` event.

## ID Format

Resource IDs use URN format: `urn:<typeSlug>:<ksuid>`

Example: `urn:blog-post:2QBGR7s8vKxLJdOmQZ0hSX1aMz9`
