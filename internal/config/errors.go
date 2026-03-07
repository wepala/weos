package config

import "errors"

var (
	// ErrMissingDatabaseDSN is returned when DatabaseDSN is not set.
	ErrMissingDatabaseDSN = errors.New("database DSN is required")

	// ErrInvalidLogLevel is returned when LogLevel has an invalid value.
	ErrInvalidLogLevel = errors.New("invalid log level, must be one of: debug, info, warn, error")
)
