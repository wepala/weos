---
title: Add an HTML Template
parent: How-to Guides
layout: default
nav_order: 6
---

# Add an HTML Template

Templates bind resource data to HTML using `data-weos-*` attributes.

## Step 1: Install the Website Preset

```bash
weos resource-type preset install website
```

This gives you the `web-page-template` type.

## Step 2: Create the Template

Write your HTML with `data-weos-entity` and `data-weos-slot` attributes:

```html
<article data-weos-entity="BlogPosting">
  <h1 data-weos-slot="headline"></h1>
  <div class="meta">
    <span data-weos-slot="author"></span>
    <time data-weos-slot="datePublished"></time>
  </div>
  <div class="body" data-weos-slot="articleBody"></div>
</article>
```

## Step 3: Register as a Resource

```bash
weos resource create --type web-page-template \
  --data '{
    "name": "Blog Post Layout",
    "templateBody": "<article data-weos-entity=\"BlogPosting\"><h1 data-weos-slot=\"headline\"></h1><div data-weos-slot=\"articleBody\"></div></article>",
    "slots": ["headline", "articleBody", "author", "datePublished"]
  }'
```

The `slots` array declares supported content slots, which the admin UI uses to generate form fields.

## Step 4: Assign to a Page

```bash
# Get the template ID from the previous step
weos resource create --type web-page \
  --data '{"name": "Blog", "slug": "blog", "template": "TEMPLATE_ID"}'
```

## Attribute Reference

| Attribute | Purpose | Value |
|-----------|---------|-------|
| `data-weos-entity` | Declares the Schema.org type | Type name (e.g., `Product`, `BlogPosting`) |
| `data-weos-slot` | Binds to a content property | Property name (e.g., `headline`, `hero.title`) |

See [Template Attributes]({% link _reference/data-weos-attributes.md %}) for the full reference.
