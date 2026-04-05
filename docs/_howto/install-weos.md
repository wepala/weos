---
title: Install WeOS
parent: How-to Guides
layout: default
nav_order: 1
---

# Install WeOS

## From Source

Requires Go 1.25+.

```bash
git clone https://github.com/wepala/weos.git
cd weos
make deps
make build
./bin/weos --version
```

The binary is at `bin/weos`.

## From GitHub Releases

Download the latest release binary for your platform from [GitHub Releases](https://github.com/wepala/weos/releases).

```bash
# Example for macOS (adjust URL for your platform)
chmod +x weos
./weos --version
```

## With Docker

```bash
docker build -t weos .
docker run -p 8080:8080 weos
```

The Dockerfile performs a multi-stage build:
1. Builds the Nuxt 3 frontend (`web/admin/`)
2. Compiles the Go binary with the embedded frontend
3. Produces a minimal Alpine runtime image

### Docker with a persistent database

```bash
docker run -p 8080:8080 \
  -v $(pwd)/data:/data \
  -e DATABASE_DSN=/data/weos.db \
  weos
```

## Verify

```bash
weos --version
# weos version 0.1.0

weos serve &
curl http://localhost:8080/api/health
# {"status": "ok"}
```
