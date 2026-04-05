---
title: Manage Persons and Organizations
parent: How-to Guides
layout: default
nav_order: 7
---

# Manage Persons and Organizations

Persons and organizations are core resource types (auto-installed). They have dedicated CLI commands and API endpoints.

## Persons

### Create

```bash
# CLI
weos person create --given-name Jane --family-name Smith --email jane@example.com

# API
curl -X POST http://localhost:8080/api/persons \
  -H "Content-Type: application/json" \
  -d '{"given_name": "Jane", "family_name": "Smith", "email": "jane@example.com"}'
```

The `name` field is auto-computed: `"Jane Smith"`.

### List

```bash
# CLI
weos person list
weos person list --limit 50

# API
curl http://localhost:8080/api/persons
```

### Get

```bash
# CLI
weos person get urn:person:abc123

# API
curl http://localhost:8080/api/persons/urn:person:abc123
```

### Delete

```bash
# CLI
weos person delete urn:person:abc123

# API
curl -X DELETE http://localhost:8080/api/persons/urn:person:abc123
```

## Organizations

### Create

```bash
# CLI
weos organization create --name "Acme Corp" --slug acme

# API (alias: org)
curl -X POST http://localhost:8080/api/organizations \
  -H "Content-Type: application/json" \
  -d '{"name": "Acme Corp", "slug": "acme"}'
```

### List

```bash
weos org list
curl http://localhost:8080/api/organizations
```

### List Members

```bash
curl http://localhost:8080/api/organizations/urn:organization:abc123/members
```

### Delete

```bash
weos org delete urn:organization:abc123
```

## Person vs. User

- **Person** — a domain entity (FOAF/Schema.org). Represents a person in your content model.
- **User** — an auth entity. Represents someone who can log in.

They're separate resource types. A person exists in your content; a user exists in your auth system. They may or may not correspond to the same real person.
