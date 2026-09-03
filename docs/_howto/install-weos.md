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
docker build --build-arg VERSION="$(git describe --tags --dirty)" -t weos .
docker run -p 8080:8080 weos
```

`.dockerignore` excludes `.git`, so the builder cannot run `git describe` itself.
Hand the version in with `--build-arg` or the image reports `dev` and a running
container cannot name the tag it came from.

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
# weos version v3.0.1-beta.1                <- built on a tag
# weos version v3.0.1-alpha21-16-gee43392   <- built 16 commits past a tag
# weos version dev+5a14625a3469             <- no tag stamped, commit recorded
# weos version dev                          <- no tag stamped, no commit recorded

weos serve &
curl http://localhost:8080/api/health
# {"status": "ok"}
```

There is no fixed version to check against here. What a binary can say depends on
how it was built. The From Source path above runs `make build`, which stamps
`git describe --tags --dirty`, so it prints the first form on a tag and the
second one anywhere past it. A release download and a
`go install github.com/wepala/weos/v3/cmd/weos@<tag>` print the tag on its own.
Anything unstamped reports `dev`, plus the commit where the Go toolchain recorded
one. The Running WeOS tutorial has the full rule.
