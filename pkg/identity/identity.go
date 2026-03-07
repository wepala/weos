package identity

import (
	"os"
	"strings"

	"github.com/segmentio/ksuid"
)

// DefaultBasePath is the default base path for entity IDs.
const DefaultBasePath = "https://example.com/weos"

// basePath holds the configured base path, defaulting to DefaultBasePath.
// It can be overridden via the IDENTITY_BASE_PATH environment variable or SetBasePath.
var basePath = DefaultBasePath

func init() {
	if bp := os.Getenv("IDENTITY_BASE_PATH"); bp != "" {
		basePath = bp
	}
}

// SetBasePath overrides the base path used for generating entity IDs.
// This is useful for tests or multi-tenant configurations.
func SetBasePath(bp string) {
	basePath = bp
}

// BasePath returns the currently configured base path.
func BasePath() string {
	return basePath
}

// Add your entity type constants here, e.g.:
// const (
// 	TypeUser    = "user"
// 	TypeProduct = "product"
// )

// New generates a URL-based entity ID: {basePath}/{entityType}/{ksuid}.
func New(entityType string) string {
	return basePath + "/" + entityType + "/" + ksuid.New().String()
}

// NewSlug generates a URL-based entity ID with a well-known slug: {basePath}/{entityType}/{slug}.
func NewSlug(entityType, slug string) string {
	return basePath + "/" + entityType + "/" + slug
}

// ExtractKSUID returns the last path segment of an entity ID.
func ExtractKSUID(id string) string {
	if idx := strings.LastIndex(id, "/"); idx >= 0 && idx < len(id)-1 {
		return id[idx+1:]
	}
	return id
}

// ExtractEntityType returns the second-to-last path segment of an entity ID.
func ExtractEntityType(id string) string {
	lastSlash := strings.LastIndex(id, "/")
	if lastSlash <= 0 {
		return ""
	}
	prefix := id[:lastSlash]
	secondSlash := strings.LastIndex(prefix, "/")
	if secondSlash < 0 {
		return prefix
	}
	return prefix[secondSlash+1:]
}
