// Copyright (c) Bas van Beek 2024.
// Copyright (c) Tetrate, Inc 2023.
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

// Package function provides an implementation of the telemetry.Logger interface
// that uses a given function to emit logs.
package function

import (
	"context"
	"sync/atomic"

	"github.com/basvanbeek/telemetry"
)

type (
	// Emit is a function that will be used to produce log messages by the function Logger.
	// Implementations of this function just need to implement the log writing. Decisions on whether to
	// emit a log or not based on the log level should not be made here as the function Logger already
	// takes care of that.
	// Similarly, the keyValues parameter presented in this method will already contain al the key/value pairs
	// that need to be logged.
	// The function will only be called when the log actually needs to be emitted.
	Emit func(level telemetry.Level, msg string, err error, values Values, callerSkip int)

	// Values contains all the key/value pairs to be included when emitting logs.
	Values struct {
		// FromContext has all the key/value pairs that have been added to the Logger Context
		FromContext []interface{}
		// FromLogger has all the key/value pairs that have been added to the Logger object itself
		FromLogger []interface{}
		// FromMethod has the key/value pairs that were passed to the logging method.
		FromMethod []interface{}
	}

	// Logger is an implementation of the telemetry.Logger that allows configuring named
	// loggers that can be configured independently and referenced by name.
	Logger struct {
		// ctx holds the Context to extract key-value pairs from to be added to each
		// log line.
		ctx context.Context
		// args holds the key-value pairs to be added to each log line.
		args []interface{}
		// metric holds the Metric to increment each time Info() or Error() is called.
		metric telemetry.Metric
		// level holds the configured log level.
		level *int32
		// emitFunc is the function that will be used to actually emit the logs
		emitFunc Emit
		// callerSkip is the number of stack frames to skip when adding file and line.
		callerSkip int32
	}
)

// compile time check for compatibility with the telemetry.Logger interface.
var _ telemetry.Logger = (*Logger)(nil)

// NewLogger creates a new function Logger that uses the given Emit function to write log messages.
// Loggers are configured at telemetry.LevelInfo level by default.
func NewLogger(emitFunc Emit, callerSkip int) telemetry.Logger {
	lvl := int32(telemetry.LevelInfo)
	return &Logger{
		ctx:        context.Background(),
		level:      &lvl,
		emitFunc:   emitFunc,
		callerSkip: int32(callerSkip),
	}
}

func (l *Logger) CSIncrease() {
	atomic.AddInt32(&l.callerSkip, 1)
}

func (l *Logger) CSDecrease() {
	atomic.AddInt32(&l.callerSkip, -1)
}

// Debug emits a log message at debug level with the given key value pairs.
func (l *Logger) Debug(msg string, keyValues ...interface{}) {
	if !l.enabled(telemetry.LevelDebug) {
		return
	}
	l.emit(telemetry.LevelDebug, msg, nil, keyValues)
}

// Info emits a log message at info level with the given key value pairs.
func (l *Logger) Info(msg string, keyValues ...interface{}) {
	// even if we don't output the log line due to the level configuration,
	// we always emit the Metric if it is set.
	if l.metric != nil {
		l.metric.RecordContext(l.ctx, 1)
	}
	if !l.enabled(telemetry.LevelInfo) {
		return
	}
	l.emit(telemetry.LevelInfo, msg, nil, keyValues)
}

// Error emits a log message at error level with the given key value pairs.
// The given error will be used as the last parameter in the message format
// string.
func (l *Logger) Error(msg string, err error, keyValues ...interface{}) {
	// even if we don't output the log line due to the level configuration,
	// we always emit the Metric if it is set.
	if l.metric != nil {
		l.metric.RecordContext(l.ctx, 1)
	}

	if !l.enabled(telemetry.LevelError) {
		return
	}

	l.emit(telemetry.LevelError, msg, err, keyValues)
}

// emit the given log with all the key/values that have been accumulated.
func (l *Logger) emit(level telemetry.Level, msg string, err error, keyValues []interface{}) {
	// Note that here we don't ensure an even number of arguments in the keyValues slice.
	// We let that to the emit function implementation with the idea of being able to accommodate
	// unstructured loggers that don't use arguments as key/value pairs.
	l.emitFunc(level, msg, err, Values{
		FromContext: telemetry.KeyValuesFromContext(l.ctx),
		FromLogger:  l.args,
		FromMethod:  keyValues,
	}, int(l.callerSkip))
}

// Level returns the logging level configured for this Logger.
func (l *Logger) Level() telemetry.Level { return telemetry.Level(atomic.LoadInt32(l.level)) }

// SetLevel configures the logging level for the Logger.
func (l *Logger) SetLevel(level telemetry.Level) {
	switch {
	case level < telemetry.LevelError:
		level = telemetry.LevelNone
	case level < telemetry.LevelInfo:
		level = telemetry.LevelError
	case level < telemetry.LevelDebug:
		level = telemetry.LevelInfo
	default:
		level = telemetry.LevelDebug
	}

	atomic.StoreInt32(l.level, int32(level))
}

// enabled checks if the current Logger should emit log messages for the given
// logging level.
func (l *Logger) enabled(level telemetry.Level) bool { return l.emitFunc != nil && level <= l.Level() }

// With returns Logger with provided key value pairs attached.
func (l *Logger) With(keyValues ...interface{}) telemetry.Logger {
	if len(keyValues) == 0 {
		return l
	}
	if len(keyValues)%2 != 0 {
		keyValues = append(keyValues, "(MISSING)")
	}

	// We don't call Clone() here as we don't want to deference the level pointer;
	// we just want to add the given args.
	newLogger := newLoggerWithValues(l.ctx, l.metric, l.level, l.emitFunc, l.args, l.callerSkip)

	for i := 0; i < len(keyValues); i += 2 {
		if k, ok := keyValues[i].(string); ok {
			newLogger.args = append(newLogger.args, k, keyValues[i+1])
		}
	}

	return newLogger
}

// Context attaches provided Context to the Logger allowing metadata found in
// this context to be used for log lines and metrics labels.
func (l *Logger) Context(ctx context.Context) telemetry.Logger {
	// We don't call Clone() here as we don't want to deference the level pointer;
	// we just want to set the context.
	return newLoggerWithValues(ctx, l.metric, l.level, l.emitFunc, l.args, l.callerSkip)
}

// Metric attaches provided Metric to the Logger allowing this metric to
// record each invocation of Info and Error log lines. If context is available
// in the Logger, it can be used for Metrics labels.
func (l *Logger) Metric(m telemetry.Metric) telemetry.Logger {
	// We don't call Clone() here as we don't want to deference the level pointer;
	// we just want to set the metric.
	return newLoggerWithValues(l.ctx, m, l.level, l.emitFunc, l.args, l.callerSkip)
}

// Clone the current Logger and return it
func (l *Logger) Clone() telemetry.Logger {
	// When cloning the logger, we don't want both logger to share a level.
	// We need to dereference the pointer and set the level properly.
	lvl := *l.level
	return newLoggerWithValues(l.ctx, l.metric, &lvl, l.emitFunc, l.args, l.callerSkip)
}

// newLoggerWithValues creates a new instance of a logger with the given data.
func newLoggerWithValues(ctx context.Context, m telemetry.Metric, l *int32, f Emit, args []interface{}, cs int32) *Logger {
	newLogger := &Logger{
		args:       make([]interface{}, len(args)),
		ctx:        ctx,
		metric:     m,
		level:      l,
		emitFunc:   f,
		callerSkip: cs,
	}
	copy(newLogger.args, args)
	return newLogger
}
