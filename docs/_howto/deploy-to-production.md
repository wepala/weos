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
docker build -t weos .
```

The multi-stage Dockerfile:
1. Builds the Nuxt 3 admin frontend
2. Compiles the Go binary with embedded frontend
3. Produces a minimal Alpine image (~50MB) running as a non-root user on port 8080

## Deploy to Google Cloud Run

```bash
# Build and push
docker build -t gcr.io/YOUR_PROJECT/weos .
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

The repository includes a `.github/workflows/deploy.yml` that automates this with GitHub Actions.

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
- [ ] Set `LOG_LEVEL=info` or `warn` (not `debug`)
- [ ] Run behind a reverse proxy with TLS termination
- [ ] Enable BigQuery dual-write if you want event analytics

See [Environment Variables]({% link _reference/environment-variables.md %}) for the full list.
