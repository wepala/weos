# Stage 1: Build frontend (Nuxt 3 SPA)
FROM node:20-alpine AS frontend
WORKDIR /app/web/admin
COPY web/admin/package.json web/admin/package-lock.json ./
RUN npm ci
COPY web/admin/ ./
RUN npx nuxt generate

# Stage 2: Build Go binary (pure Go — the glebarez SQLite driver needs no cgo)
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/web/admin/.output/public/ ./web/dist/
# The tag this image was built from, so the binary can name it. .dockerignore
# excludes .git, so `git describe` cannot run in here and the version has to be
# handed in:  docker build --build-arg VERSION="$(git describe --tags --dirty)" .
# Declared here rather than beside FROM so a new version does not invalidate the
# cached `go mod download` layer above.
#
# Left unset the build passes no -X, and the binary falls back to reporting
# `dev` from the build info the toolchain records. See internal/version.
ARG VERSION=""
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags "${VERSION:+-X github.com/wepala/weos/v3/internal/version.version=$VERSION}" \
    -o /weos ./cmd/weos

# Stage 3: Minimal runtime
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 1000 appuser
COPY --from=builder /weos /weos
COPY --chmod=755 entrypoint.sh /entrypoint.sh
USER appuser
EXPOSE 8080
ENTRYPOINT ["/entrypoint.sh"]
