package entities

import "context"

// Logger defines the application logging interface.
// Implementations should be provided in the infrastructure layer.
type Logger interface {
	Info(ctx context.Context, msg string, fields ...interface{})
	Warn(ctx context.Context, msg string, fields ...interface{})
	Error(ctx context.Context, msg string, fields ...interface{})
}
