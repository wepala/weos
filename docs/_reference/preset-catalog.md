---
title: Preset Catalog
parent: Reference
layout: default
nav_order: 5
---

# Preset Catalog

Presets bundle related resource types. Install a preset with:

```bash
weos resource-type preset install <name>
```

## core

**Auto-install:** Yes (installed on first run)

| Type | Slug | @type | Properties |
|------|------|-------|------------|
| Person | `person` | foaf:Person / schema:Person | `givenName`\*, `familyName`\*, `name` (computed), `email`, `avatarURL` |
| Organization | `organization` | org:Organization / schema:Organization | `name`\*, `slug`\*, `description`, `url`, `logoURL` |

The Person type auto-computes `name` from `givenName` + `familyName`.

---

## ecommerce

**Auto-install:** No

| Type | Slug | @type | Properties |
|------|------|-------|------------|
| Product | `product` | schema:Product | `name`\*, `description`, `sku`, `brand`, `image` (format: uri) |
| Offer | `offer` | schema:Offer | `name`\*, `price`\* (number), `priceCurrency`, `availability` |
| Review | `review` | schema:Review | `name`\*, `reviewBody`, `reviewRating` (integer), `author` |
| Service | `service` | schema:Service | `name`\*, `description`, `provider`, `serviceType` |

---

## tasks

**Auto-install:** No

| Type | Slug | @type | Properties |
|------|------|-------|------------|
| Project | `project` | schema:Project | `name`\*, `description`, `status` |
| Task | `task` | schema:Action | `name`\*, `description`, `status`, `priority`, `dueDate` (format: date), `project` (ref→project, display: name) |

The Task type's `project` property references the Project type, creating a foreign key relationship.

---

## website

**Auto-install:** No

| Type | Slug | @type | Properties |
|------|------|-------|------------|
| Web Site | `web-site` | schema:WebSite | `name`\*, `url` (format: uri), `description`, `inLanguage` |
| Web Page | `web-page` | schema:WebPage | `name`\*, `slug`, `description`, `template` |
| Web Page Element | `web-page-element` | schema:WebPageElement | `name`\*, `cssSelector`, `content` |
| Web Page Template | `web-page-template` | schema:WebPage (variant: template) | `name`\*, `templateBody`, `slots` (array of strings) |
| Theme | `theme` | schema:CreativeWork | `name`\*, `version`, `thumbnailUrl` (format: uri) |
| Article | `article` | schema:Article | `headline`\*, `articleBody`, `author`, `datePublished` (format: date-time) |
| Blog Post | `blog-post` | schema:BlogPosting | `headline`\*, `articleBody`, `author`, `datePublished` (format: date-time) |
| FAQ | `faq` | schema:FAQPage | `name`\*, `mainEntity` (array of {name, acceptedAnswer}) |
| Breadcrumb List | `breadcrumb-list` | schema:BreadcrumbList | `name`\*, `itemListElement` (array of {name, item (uri), position (int)}) |

---

## events

**Auto-install:** No

| Type | Slug | @type | Properties |
|------|------|-------|------------|
| Event | `event` | schema:Event | `name`\*, `description`, `startDate` (format: date-time), `endDate` (format: date-time), `location` |
| Place | `place` | schema:Place | `name`\*, `address`, `geo` (object: latitude, longitude) |
| Venue | `venue` | schema:EventVenue | `name`\*, `address`, `maximumAttendeeCapacity` (integer) |

---

## knowledge

**Auto-install:** No

| Type | Slug | @type | Properties |
|------|------|-------|------------|
| Concept | `concept` | skos:Concept | `prefLabel`\*, `altLabel` (array), `definition` |
| Concept Scheme | `concept-scheme` | skos:ConceptScheme | `title`\*, `description` |
| Collection | `collection` | skos:Collection | `prefLabel`\*, `member` (array) |

---

\* = required property

## JSON Schema Extensions

| Extension | Purpose | Example |
|-----------|---------|---------|
| `x-resource-type` | Foreign key to another resource type | `"x-resource-type": "project"` |
| `x-display-property` | Which property of the referenced type to show | `"x-display-property": "name"` |
