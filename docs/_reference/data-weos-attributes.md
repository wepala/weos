---
title: Template Attributes
parent: Reference
layout: default
nav_order: 6
---

# Template Attributes

WeOS templates use `data-weos-*` HTML attributes to bind content from resources to DOM elements. These attributes tell WeOS which part of a resource's data should be rendered in each element.

## Attributes

### `data-weos-entity`

Binds an HTML element to a Schema.org entity type. This declares what *kind* of content the element represents.

**Value:** A Schema.org type name (e.g., `Product`, `BlogPosting`, `Event`)

```html
<article data-weos-entity="BlogPosting">
  <!-- Content slots go here -->
</article>
```

### `data-weos-slot`

Binds an element to a named content slot within the entity. The slot name corresponds to a property of the entity.

**Value:** A property name, optionally using dot notation for nested slots (e.g., `headline`, `hero.headline`)

```html
<article data-weos-entity="BlogPosting">
  <h1 data-weos-slot="headline"></h1>
  <div data-weos-slot="articleBody"></div>
  <span data-weos-slot="author"></span>
  <time data-weos-slot="datePublished"></time>
</article>
```

## Usage Example

A complete product page template:

```html
<div data-weos-entity="Product">
  <h1 data-weos-slot="name"></h1>
  <img data-weos-slot="image" alt="">
  <p data-weos-slot="description"></p>
  <span data-weos-slot="sku"></span>
  <span data-weos-slot="brand"></span>
</div>
```

## Creating Templates as Resources

Templates can be stored as `web-page-template` resources (from the website preset):

```bash
weos resource create --type web-page-template \
  --data '{
    "name": "Product Page",
    "templateBody": "<div data-weos-entity=\"Product\"><h1 data-weos-slot=\"name\"></h1><p data-weos-slot=\"description\"></p></div>",
    "slots": ["name", "description", "image", "sku", "brand"]
  }'
```

The `slots` array declares which content slots the template supports, helping the admin UI generate the right form fields.

## How It Works

1. WeOS reads the template HTML and finds elements with `data-weos-entity`
2. For each entity, it loads the corresponding resource data
3. It fills `data-weos-slot` elements with values from the resource's JSON-LD data
4. Property names in slots map to JSON-LD properties (which are backed by Schema.org vocabulary)
5. The rendered HTML is served as the static page

## Slot Types

The content injected depends on the HTML element:

| Element | Behavior |
|---------|----------|
| Text elements (`h1`, `p`, `span`, etc.) | Sets `textContent` |
| `<img>` | Sets `src` attribute |
| `<a>` | Sets `href` attribute |
| `<time>` | Sets `datetime` attribute and text |
| Other elements | Sets `textContent` |
