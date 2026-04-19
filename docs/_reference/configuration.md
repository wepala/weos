---
title: Configuration
parent: Reference
layout: default
nav_order: 2
---

# Configuration

WeOS configuration is loaded in layers, with each step overriding the previous.

## Loading Order

1. **`config.Default()`** — hard-coded sensible defaults for local development
2. **`godotenv.Load()`** — loads a `.env` file (if present) into the process environment
3. **`cfg.LoadFromEnvironment()`** — reads environment variables into the Config struct
4. **CLI flags** — `--database-dsn`, `--verbose` override everything

## Config Fields

| Field | Type | Default | Env Var | Description |
|-------|------|---------|---------|-------------|
| `DatabaseDSN` | string | `"weos.db"` | `DATABASE_DSN` | Database connection string |
| `LogLevel` | string | `"info"` | `LOG_LEVEL` | Logging level: `debug`, `info`, `warn`, `error` |
| `Server.Port` | int | `8080` | `SERVER_PORT` | HTTP server port |
| `Server.Host` | string | `"0.0.0.0"` | `SERVER_HOST` | HTTP server bind address |
| `SessionSecret` | string | `"change-me-in-production"` | `SESSION_SECRET` | Secret key for session cookies |
| `OAuth.GoogleClientID` | string | `""` | `GOOGLE_CLIENT_ID` | Google OAuth client ID |
| `OAuth.GoogleClientSecret` | string | `""` | `GOOGLE_CLIENT_SECRET` | Google OAuth client secret |
| `OAuth.FrontendURL` | string | `""` | `FRONTEND_URL` | Frontend URL for OAuth redirects |
| `LLM.GeminiAPIKey` | string | `""` | `GEMINI_API_KEY` | Google Gemini API key |
| `LLM.GeminiModel` | string | `"gemini-2.0-flash"` | `GEMINI_MODEL` | Gemini model ID |
| `BigQueryProjectID` | string | `""` | `BIGQUERY_PROJECT_ID` | BigQuery project ID (dual-write event store) |
| `BigQueryDatasetID` | string | `""` | `BIGQUERY_DATASET_ID` | BigQuery dataset ID |
| `BigQueryTableID` | string | `""` | `BIGQUERY_TABLE_ID` | BigQuery table ID |

## Database DSN Formats

The database driver is auto-detected from the DSN format:

**SQLite** (default):
```
weos.db
file:weos.db?cache=shared&_foreign_keys=1
```

**PostgreSQL**:
```
host=localhost user=postgres password=secret dbname=weos sslmode=disable
postgres://postgres:secret@localhost:5432/weos?sslmode=disable
```

## OAuth

OAuth is **enabled** when both `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` are set. When OAuth is disabled, the server runs in development mode with `SoftAuth` middleware (uses `X-Dev-Agent` header for identity).

## Using a .env File

Create a `.env` file in the project root:

```bash
DATABASE_DSN=weos.db
LOG_LEVEL=debug
SERVER_PORT=8080
SESSION_SECRET=my-secret-key
```

The `.env` file is loaded automatically on startup. It's listed in `.gitignore` by default.

## Validation

`Config.Validate()` checks:
- `DatabaseDSN` must not be empty → `ErrMissingDatabaseDSN`
- `LogLevel` must be one of `debug`, `info`, `warn`, `error` → `ErrInvalidLogLevel`
