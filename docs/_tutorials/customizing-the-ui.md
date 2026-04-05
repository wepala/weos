---
title: Customizing the UI
parent: Tutorials
layout: default
nav_order: 4
---

# Customizing the UI

WeOS generates admin screens automatically from your resource types. In this tutorial you'll learn how to organize the sidebar menu, control which types are visible to different roles, and work with HTML templates for the public-facing site.

## Prerequisites

- WeOS running with some resource types installed (see [Running WeOS]({% link _tutorials/running-weos.md %}))
- At least the `tasks` and `website` presets installed:
  ```bash
  ./bin/weos resource-type preset install tasks
  ./bin/weos resource-type preset install website
  ```

## The Admin UI

The WeOS admin interface is a Nuxt 3 single-page application embedded in the Go binary. It auto-generates:

- **Sidebar navigation** — lists all resource types
- **List views** — table of resources for each type, with columns from the JSON Schema
- **Create/edit forms** — form fields derived from schema properties
- **Detail views** — display resource data

## Step 1: Organize the Sidebar Menu

By default, every resource type appears in the sidebar. You can organize types into groups and hide types that shouldn't be directly accessible.

### Get current sidebar settings

```bash
curl http://localhost:8080/api/settings/sidebar
```

### Configure sidebar groups

```bash
curl -X PUT http://localhost:8080/api/settings/sidebar \
  -H "Content-Type: application/json" \
  -d '{
    "hidden_slugs": ["web-page-element", "breadcrumb-list"],
    "menu_groups": {
      "Content": ["article", "blog-post", "faq", "web-page"],
      "Site Structure": ["web-site", "web-page-template", "theme"],
      "Project Management": ["project", "task"]
    }
  }'
```

This configuration:
- **Hides** `web-page-element` and `breadcrumb-list` from the sidebar (they're still accessible via API)
- **Groups** types under named sections in the sidebar
- Types not listed in any group appear in a default "Other" section

### Role-specific sidebar views

Different roles can see different sidebar configurations. Pass the `role` query parameter:

```bash
# Get sidebar as seen by the "editor" role
curl "http://localhost:8080/api/settings/sidebar?role=editor"
```

## Step 2: HTML Templates for the Public Site

WeOS serves a static site from HTML templates. Templates use `data-weos-*` attributes to bind content from your resources.

### Template attributes

| Attribute | Purpose | Example |
|-----------|---------|---------|
| `data-weos-entity` | Binds an element to a Schema.org entity type | `data-weos-entity="Product"` |
| `data-weos-slot` | Binds an element to a named content slot | `data-weos-slot="hero.headline"` |

### Creating a template

Install the website preset if you haven't already:

```bash
./bin/weos resource-type preset install website
```

Create a Web Page Template resource:

```bash
./bin/weos resource create --type web-page-template \
  --data '{
    "name": "Blog Post Layout",
    "templateBody": "<article data-weos-entity=\"BlogPosting\"><h1 data-weos-slot=\"headline\"></h1><div data-weos-slot=\"articleBody\"></div><footer><span data-weos-slot=\"author\"></span> &mdash; <time data-weos-slot=\"datePublished\"></time></footer></article>",
    "slots": ["headline", "articleBody", "author", "datePublished"]
  }'
```

### Assigning a template to a page

```bash
# Create a web page that uses the template (replace TEMPLATE_ID)
./bin/weos resource create --type web-page \
  --data '{
    "name": "My First Blog Post",
    "slug": "hello-world",
    "template": "TEMPLATE_ID"
  }'
```

### Creating a blog post

```bash
./bin/weos resource create --type blog-post \
  --data '{
    "headline": "Hello World",
    "articleBody": "Welcome to my blog, powered by WeOS.",
    "author": "Your Name",
    "datePublished": "2026-04-05T12:00:00Z"
  }'
```

## Step 3: Managing the Theme

Themes control the visual appearance of your site:

```bash
./bin/weos resource create --type theme \
  --data '{
    "name": "Default Theme",
    "version": "1.0.0"
  }'
```

## Static Asset Embedding

The frontend assets are embedded at build time via `web/embed.go`:

```go
//go:embed all:dist
var StaticFS embed.FS
```

The SPA middleware serves these assets:
- Requests to `/api/*` go to API handlers
- All other requests serve static files from `dist/`
- Missing files fall back to `index.html` (SPA routing)
- Static assets (JS, CSS, images) receive long-lived cache headers

For development, the Nuxt 3 admin UI source is in `web/admin/`. Build it with:

```bash
make dev-build-frontend
```

## What You've Learned

- How to organize the sidebar menu with groups and hidden types
- How role-specific sidebar views work
- How HTML templates use `data-weos-*` attributes for content binding
- How themes, pages, and templates relate to each other
- How static assets are embedded and served

## What's Next

- [Template Attributes Reference]({% link _reference/data-weos-attributes.md %}) — full attribute documentation
- [Add an HTML Template]({% link _howto/add-html-template.md %}) — step-by-step template creation guide
- [Preset Catalog]({% link _reference/preset-catalog.md %}) — all website preset types
