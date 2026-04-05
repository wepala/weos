---
title: Environment Variables
parent: Reference
layout: default
nav_order: 7
---

# Environment Variables

All environment variables can be set in a `.env` file in the project root, which is loaded automatically on startup.

## Core

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `DATABASE_DSN` | string | `weos.db` | Database connection string. File paths or `file:` URIs use SQLite; `host=...` or `postgres://...` URIs use PostgreSQL. |
| `LOG_LEVEL` | string | `info` | Logging level: `debug`, `info`, `warn`, `error`. `debug` uses Zap's development config; others use production config. |

## Server

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `SERVER_PORT` | int | `8080` | HTTP server port |
| `PORT` | int | | Alternative port variable (overrides `SERVER_PORT` in serve command) |
| `SERVER_HOST` | string | `0.0.0.0` | HTTP server bind address |
| `SESSION_SECRET` | string | `change-me-in-production` | Secret key for session cookie encryption |

## Authentication (OAuth)

OAuth is enabled when both `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` are set. When disabled, the server runs in development mode.

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `GOOGLE_CLIENT_ID` | string | | Google OAuth 2.0 client ID |
| `GOOGLE_CLIENT_SECRET` | string | | Google OAuth 2.0 client secret |
| `FRONTEND_URL` | string | | Frontend URL for OAuth redirect callbacks |

## MCP Server

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `MCP_SERVICES` | string | (all) | Comma-separated list of MCP tool groups to enable: `person`, `organization`, `resource-type`, `resource` |

## LLM Integration

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `GEMINI_API_KEY` | string | | Google Gemini API key |
| `GEMINI_MODEL` | string | `gemini-2.0-flash` | Gemini model identifier |

## Analytics (Optional)

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `BIGQUERY_PROJECT_ID` | string | | Google BigQuery project ID. When set, enables dual-write event store (events written to both primary DB and BigQuery). |
| `BIGQUERY_DATASET_ID` | string | | BigQuery dataset ID |
| `BIGQUERY_TABLE_ID` | string | | BigQuery table ID |

## Example .env File

```bash
# Database
DATABASE_DSN=weos.db
LOG_LEVEL=debug

# Server
SERVER_PORT=8080
SESSION_SECRET=my-super-secret-key

# OAuth (comment out for dev mode)
# GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
# GOOGLE_CLIENT_SECRET=your-client-secret
# FRONTEND_URL=http://localhost:3000
```
