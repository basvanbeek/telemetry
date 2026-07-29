// Copyright (c) Bas van Beek 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package slog

import (
	"context"
	stdslog "log/slog"
	"strings"

	"github.com/basvanbeek/telemetry"
)

// Handler forwards slog records to a telemetry.Logger.
type Handler struct {
	logger telemetry.Logger
	attrs  []stdslog.Attr
	groups []string
}

type callerSkipper interface {
	CSIncrease()
}

const bridgeCallerSkipDelta = 3

// NewHandler creates a slog.Handler backed by telemetry.Logger.
func NewHandler(logger telemetry.Logger) *Handler {
	if logger == nil {
		logger = telemetry.NoopLogger()
	}
	return &Handler{logger: logger}
}

// Enabled reports whether the telemetry logger should emit this slog level.
func (h *Handler) Enabled(_ context.Context, level stdslog.Level) bool {
	if h.logger == nil {
		return false
	}
	return h.logger.Level() >= toTelemetryLevel(level)
}

// Handle forwards the record to the matching telemetry logging method.
func (h *Handler) Handle(ctx context.Context, record stdslog.Record) error {
	logger := h.logger
	if logger == nil {
		return nil
	}
	if ctx != nil {
		logger = logger.Context(ctx)
	}
	if _, ok := logger.(callerSkipper); ok {
		logger = logger.Clone()
		for i := 0; i < bridgeCallerSkipDelta; i++ {
			logger.(callerSkipper).CSIncrease()
		}
	}

	var (
		kvs       []interface{}
		recordErr error
	)
	h.appendAttrs(&kvs, &recordErr, h.attrs, h.groups)
	record.Attrs(func(attr stdslog.Attr) bool {
		h.appendAttrs(&kvs, &recordErr, []stdslog.Attr{attr}, h.groups)
		return true
	})

	switch toTelemetryLevel(record.Level) { //nolint:exhaustive //
	case telemetry.LevelDebug:
		logger.Debug(record.Message, kvs...)
	case telemetry.LevelInfo:
		logger.Info(record.Message, kvs...)
	case telemetry.LevelError:
		logger.Error(record.Message, recordErr, kvs...)
	default:
		logger.Error(record.Message, recordErr, kvs...)
	}
	return nil
}

// WithAttrs returns a handler that includes the provided attributes.
func (h *Handler) WithAttrs(attrs []stdslog.Attr) stdslog.Handler {
	if len(attrs) == 0 {
		return h
	}
	nextAttrs := make([]stdslog.Attr, len(h.attrs), len(h.attrs)+len(attrs))
	copy(nextAttrs, h.attrs)
	nextAttrs = append(nextAttrs, attrs...)

	return &Handler{
		logger: h.logger,
		attrs:  nextAttrs,
		groups: cloneStrings(h.groups),
	}
}

// WithGroup returns a handler with an extra attribute group prefix.
func (h *Handler) WithGroup(name string) stdslog.Handler {
	if name == "" {
		return h
	}
	nextGroups := append(cloneStrings(h.groups), name)

	return &Handler{
		logger: h.logger,
		attrs:  cloneAttrs(h.attrs),
		groups: nextGroups,
	}
}

func (h *Handler) appendAttrs(kvs *[]interface{}, recordErr *error, attrs []stdslog.Attr, groups []string) {
	for _, attr := range attrs {
		attr.Value = attr.Value.Resolve()
		if attr.Equal(stdslog.Attr{}) {
			continue
		}
		if attr.Value.Kind() == stdslog.KindGroup {
			nextGroups := groups
			if attr.Key != "" {
				nextGroups = append(cloneStrings(groups), attr.Key)
			}
			h.appendAttrs(kvs, recordErr, attr.Value.Group(), nextGroups)
			continue
		}
		if attr.Key == "" {
			continue
		}

		key := attr.Key
		if len(groups) > 0 {
			key = strings.Join(append(cloneStrings(groups), key), ".")
		}
		value := attrValue(attr.Value)
		if key == "error" {
			if errValue, ok := value.(error); ok && *recordErr == nil {
				*recordErr = errValue
				continue
			}
		}
		*kvs = append(*kvs, key, value)
	}
}

func attrValue(value stdslog.Value) interface{} {
	switch value.Kind() {
	case stdslog.KindBool:
		return value.Bool()
	case stdslog.KindDuration:
		return value.Duration()
	case stdslog.KindFloat64:
		return value.Float64()
	case stdslog.KindInt64:
		return value.Int64()
	case stdslog.KindString:
		return value.String()
	case stdslog.KindTime:
		return value.Time()
	case stdslog.KindUint64:
		return value.Uint64()
	case stdslog.KindAny:
		return value.Any()
	case stdslog.KindGroup:
		return value.Group()
	case stdslog.KindLogValuer:
		return value.LogValuer()
	default:
		return value.Any()
	}
}

func toTelemetryLevel(level stdslog.Level) telemetry.Level {
	switch level {
	case stdslog.LevelDebug:
		return telemetry.LevelDebug
	case stdslog.LevelInfo:
		return telemetry.LevelInfo
	case stdslog.LevelWarn:
		return telemetry.LevelInfo
	case stdslog.LevelError:
		return telemetry.LevelError
	default:
		return telemetry.LevelInfo
	}
}

func cloneAttrs(attrs []stdslog.Attr) []stdslog.Attr {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]stdslog.Attr, len(attrs))
	copy(out, attrs)
	return out
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

var _ stdslog.Handler = (*Handler)(nil)
