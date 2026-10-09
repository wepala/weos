---
title: Deploy to Production
parent: How-to Guides
layout: default
nav_order: 5
---

# Deploy to Production

WeOS ships as a single binary or Docker image. Deploy it anywhere that runs containers or Linux binaries.

## Docker Build

```bash
docker build --build-arg VERSION="$(git describe --tags --dirty)" -t weos .
```

`.dockerignore` excludes `.git`, so the builder cannot derive a version on its
own. Pass it in with `--build-arg` and the deployed instance can name the tag it
is running; leave it out and every image reports `dev`.

The multi-stage Dockerfile:
1. Builds the Nuxt 3 admin frontend
2. Compiles the Go binary with embedded frontend
3. Produces a minimal Alpine image (~50MB) running as a non-root user on port 8080

## Deploy to Google Cloud Run

```bash
# Build and push
docker build --build-arg VERSION="$(git describe --tags --dirty)" -t gcr.io/YOUR_PROJECT/weos .
docker push gcr.io/YOUR_PROJECT/weos

# Deploy
gcloud run deploy weos \
  --image gcr.io/YOUR_PROJECT/weos \
  --port 8080 \
  --set-env-vars "DATABASE_DSN=postgres://user:pass@host/weos" \
  --set-env-vars "SESSION_SECRET=your-secret" \
  --set-env-vars "GOOGLE_CLIENT_ID=your-id" \
  --set-env-vars "GOOGLE_CLIENT_SECRET=your-secret" \
  --set-env-vars "FRONTEND_URL=https://your-domain.run.app"
```

## Deploy to Any Container Host

WeOS is a standard Docker image. It works on:
- AWS ECS / Fargate
- Azure Container Apps
- DigitalOcean App Platform
- Fly.io
- Railway
- Any Kubernetes cluster

Required environment variables for production:

| Variable | Required | Description |
|----------|----------|-------------|
| `DATABASE_DSN` | Yes | PostgreSQL connection string |
| `SESSION_SECRET` | Yes | Random string for cookie encryption |
| `GOOGLE_CLIENT_ID` | Yes | OAuth client ID |
| `GOOGLE_CLIENT_SECRET` | Yes | OAuth client secret |
| `FRONTEND_URL` | Yes | Public URL for OAuth redirects |

## Deploy as a Binary

Download the release binary or build from source:

```bash
make build
DATABASE_DSN="postgres://..." SESSION_SECRET="..." ./bin/weos serve
```

## Production Checklist

- [ ] Use PostgreSQL (not SQLite) for concurrent access
- [ ] Set a strong `SESSION_SECRET` (not the default)
- [ ] Configure OAuth (`GOOGLE_CLIENT_ID` + `GOOGLE_CLIENT_SECRET`)
- [ ] Set `FRONTEND_URL` to your public domain
- [ ] Set `INSTANCE_ADMIN_ACCOUNT` to the operator's account id, so only its owners and admins can change resource types (see [Set the instance admin account](#set-the-instance-admin-account) — the account exists only after the operator's first sign-in)
- [ ] Set `LOG_LEVEL=info` or `warn` (not `debug`)
- [ ] Run behind a reverse proxy with TLS termination
- [ ] Enable BigQuery dual-write if you want event analytics

See [Environment Variables]({% link _reference/environment-variables.md %}) for the full list.

## Set the instance admin account

`INSTANCE_ADMIN_ACCOUNT` names an account, and on a fresh instance that account does not exist until the operator signs in for the first time. Set it in this order, before you announce the instance:

1. Deploy without `INSTANCE_ADMIN_ACCOUNT`. `serve` logs a warning that any signed-in account can change the resource types; that is expected for now.
2. Sign in as the operator. The first sign-in makes an account that the operator owns.
3. Call `GET /api/auth/me` with that session. Copy `data.account_id`. Do not copy `data.id`: that is the operator's own id, not the account's.
4. Set `INSTANCE_ADMIN_ACCOUNT` to that value.
5. Restart `serve`. It logs that resource type and preset changes are limited to the instance admin account.

Until step 5, anyone who signs in can change the resource types, so keep the gap short and do not let others sign in during it. If `serve` logs that `INSTANCE_ADMIN_ACCOUNT` names an account that does not exist, the value is wrong: every change to resource types is refused, the operator's too. Do the steps again from step 3.
