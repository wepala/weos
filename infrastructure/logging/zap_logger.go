// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package logging

import (
	"context"
	"fmt"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

// ZapLogger is a zap-based implementation of entities.Logger.
type ZapLogger struct {
	logger *zap.Logger
}

// NewZapLogger creates a new ZapLogger instance.
func NewZapLogger(logger *zap.Logger) entities.Logger {
	return &ZapLogger{
		logger: logger,
	}
}

// Info logs an informational message with optional key-value fields.
func (z *ZapLogger) Info(_ context.Context, msg string, fields ...interface{}) {
	zapFields := z.convertFields(fields...)
	z.logger.Info(msg, zapFields...)
}

// Warn logs a warning message with optional key-value fields.
func (z *ZapLogger) Warn(_ context.Context, msg string, fields ...interface{}) {
	zapFields := z.convertFields(fields...)
	z.logger.Warn(msg, zapFields...)
}

// Error logs an error message with optional key-value fields.
func (z *ZapLogger) Error(_ context.Context, msg string, fields ...interface{}) {
	zapFields := z.convertFields(fields...)
	z.logger.Error(msg, zapFields...)
}

// convertFields converts variadic key-value pairs to zap fields.
// Fields are expected in pairs: key1, value1, key2, value2, ...
func (z *ZapLogger) convertFields(fields ...interface{}) []zap.Field {
	if len(fields) == 0 {
		return nil
	}

	zapFields := make([]zap.Field, 0, len(fields)/2)
	for i := 0; i < len(fields)-1; i += 2 {
		key, ok := fields[i].(string)
		if !ok {
			continue
		}
		value := fields[i+1]
		zapFields = append(zapFields, zap.Any(key, value))
	}

	if len(fields)%2 == 1 {
		zapFields = append(zapFields, zap.Any("extra", fields[len(fields)-1]))
	}

	return zapFields
}

// ZapLoggerResult holds the zap logger result.
type ZapLoggerResult struct {
	fx.Out
	ZapLogger *zap.Logger
}

// ProvideZapLogger creates a zap logger instance based on config log level.
func ProvideZapLogger(params struct {
	fx.In
	Config config.Config
}) (ZapLoggerResult, error) {
	var logger *zap.Logger
	var err error

	logLevel := params.Config.LogLevel
	if logLevel == "" {
		logLevel = "info"
	}

	switch logLevel {
	case "debug":
		logger, err = zap.NewDevelopment()
	default:
		logger, err = zap.NewProduction()
	}

	if err != nil {
		return ZapLoggerResult{}, fmt.Errorf("failed to create zap logger: %w", err)
	}
	return ZapLoggerResult{
		ZapLogger: logger,
	}, nil
}

// LoggerResult holds the logger result.
type LoggerResult struct {
	fx.Out
	Logger entities.Logger
}

// ProvideLogger creates an entities.Logger from a *zap.Logger.
func ProvideLogger(params struct {
	fx.In
	ZapLogger *zap.Logger
}) LoggerResult {
	return LoggerResult{
		Logger: NewZapLogger(params.ZapLogger),
	}
}
