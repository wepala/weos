---
title: Seed Development Data
parent: How-to Guides
layout: default
nav_order: 8
---

# Seed Development Data

The seed command populates your local database with sample users, presets, and content.

## Run the Seed

```bash
make dev-seed
```

Or directly:

```bash
make build
./bin/weos seed
```

## What Gets Created

1. **Users:**
   - `admin@weos.dev` with the "admin" role
   - `member@weos.dev` with the "member" role

2. **Presets:** The `tasks` preset (Project and Task types)

3. **Sample Data:**
   - 2 projects: "WeOS Development", "Marketing Site"
   - 4 tasks with varying status and priority:
     - "Set up CI pipeline" (done, high)
     - "Add resource permissions API" (in-progress, high)
     - "Design landing page" (open, medium)
     - "Write blog post" (open, low)

4. **Authorization policies** for the admin role

## Manifest File

The seed writes a `.dev-seed.json` manifest containing IDs of all created entities. This file is gitignored.

## Idempotent

The seed is safe to run multiple times. It uses `FindOrCreateAgent` for users, so existing users won't be duplicated.

## Full Dev Setup

To seed and immediately start the server:

```bash
make dev-setup
```

This runs `dev-seed` then `dev-serve` (server with OAuth disabled).

## Clean and Reseed

```bash
make dev-clean   # removes weos.db, .dev-seed.json, bin/
make dev-setup   # rebuild, reseed, serve
```
