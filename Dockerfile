# Stage 1: Build frontend (Nuxt 3 SPA)
FROM node:20-alpine AS frontend
WORKDIR /app/web/admin
COPY web/admin/package.json web/admin/package-lock.json ./
RUN npm ci
COPY web/admin/ ./
RUN npx nuxt generate

# Stage 2: Build Go binary
FROM golang:1.25-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/web/admin/.output/public/ ./web/dist/
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o /weos ./cmd/weos

# Stage 3: Minimal runtime
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 1000 appuser
COPY --from=builder /weos /weos
COPY --chmod=755 entrypoint.sh /entrypoint.sh
USER appuser
EXPOSE 8080
ENTRYPOINT ["/entrypoint.sh"]
