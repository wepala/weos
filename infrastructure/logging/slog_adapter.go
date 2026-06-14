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
	"log/slog"

	"github.com/wepala/weos/v3/domain/entities"
)

// NewSlogLogger adapts an entities.Logger into a *slog.Logger. Pericarp's
// subscriber runtime logs through *slog.Logger (batch failures, lifecycle, and
// — importantly for alerting — parked poison events at error level). Bridging
// it into the weos logger keeps that output in one structured stream instead of
// leaking to slog.Default().
func NewSlogLogger(logger entities.Logger) *slog.Logger {
	return slog.New(&slogHandler{logger: logger})
}

// slogHandler forwards slog records to an entities.Logger, mapping slog levels
// onto the four weos log methods and flattening attributes into the variadic
// key/value pairs the weos logger expects. Attribute groups (WithGroup) are
// preserved by prefixing keys with the dotted group path, so a future caller
// that nests attributes does not silently collide distinct keys.
type slogHandler struct {
	logger entities.Logger
	// attrs are the With-bound attributes, keys already carrying any group
	// prefix that was active when they were added.
	attrs []slog.Attr
	// prefix is the dotted group path applied to subsequently-added and record
	// attribute keys (empty at the root).
	prefix string
}

func (h *slogHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *slogHandler) Handle(ctx context.Context, r slog.Record) error {
	fields := make([]any, 0, (len(h.attrs)+r.NumAttrs())*2)
	for _, a := range h.attrs {
		fields = append(fields, a.Key, a.Value.Any())
	}
	r.Attrs(func(a slog.Attr) bool {
		fields = append(fields, h.prefix+a.Key, a.Value.Any())
		return true
	})
	switch {
	case r.Level >= slog.LevelError:
		h.logger.Error(ctx, r.Message, fields...)
	case r.Level >= slog.LevelWarn:
		h.logger.Warn(ctx, r.Message, fields...)
	case r.Level >= slog.LevelInfo:
		h.logger.Info(ctx, r.Message, fields...)
	default:
		h.logger.Debug(ctx, r.Message, fields...)
	}
	return nil
}

func (h *slogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	for _, a := range attrs {
		merged = append(merged, slog.Attr{Key: h.prefix + a.Key, Value: a.Value})
	}
	return &slogHandler{logger: h.logger, attrs: merged, prefix: h.prefix}
}

// WithGroup nests subsequent attributes under name by extending the dotted key
// prefix. An empty name is a no-op, per the slog.Handler contract.
func (h *slogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &slogHandler{logger: h.logger, attrs: h.attrs, prefix: h.prefix + name + "."}
}
