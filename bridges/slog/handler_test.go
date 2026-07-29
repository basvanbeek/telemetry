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
	"errors"
	stdslog "log/slog"
	"reflect"
	"sync"
	"testing"

	"github.com/basvanbeek/telemetry"
)

type logEntry struct {
	level string
	msg   string
	err   error
	kvs   []interface{}
}

type loggerSpy struct {
	level telemetry.Level
	with  []interface{}
	ctx   context.Context
	store *logStore
}

type logStore struct {
	mu      sync.Mutex
	entries []logEntry
}

func newLoggerSpy(level telemetry.Level) *loggerSpy {
	return &loggerSpy{
		level: level,
		store: &logStore{},
	}
}

func (l *loggerSpy) Debug(msg string, keyValuePairs ...interface{}) {
	l.storeEntry("debug", msg, nil, keyValuePairs)
}

func (l *loggerSpy) Info(msg string, keyValuePairs ...interface{}) {
	l.storeEntry("info", msg, nil, keyValuePairs)
}

func (l *loggerSpy) Error(msg string, err error, keyValuePairs ...interface{}) {
	l.storeEntry("error", msg, err, keyValuePairs)
}

func (l *loggerSpy) SetLevel(level telemetry.Level) {
	l.level = level
}

func (l *loggerSpy) Level() telemetry.Level {
	return l.level
}

func (l *loggerSpy) With(keyValuePairs ...interface{}) telemetry.Logger {
	next := l.Clone().(*loggerSpy)
	next.with = append(next.with, keyValuePairs...)
	return next
}

func (l *loggerSpy) Context(ctx context.Context) telemetry.Logger {
	next := l.Clone().(*loggerSpy)
	next.ctx = ctx
	return next
}

func (*loggerSpy) Metric(telemetry.Metric) telemetry.Logger {
	return nil
}

func (l *loggerSpy) Clone() telemetry.Logger {
	with := make([]interface{}, len(l.with))
	copy(with, l.with)
	return &loggerSpy{
		level: l.level,
		with:  with,
		ctx:   l.ctx,
		store: l.store,
	}
}

func (l *loggerSpy) storeEntry(level, msg string, err error, keyValuePairs []interface{}) {
	args := make([]interface{}, 0, len(l.with)+len(keyValuePairs)+4)
	args = append(args, l.with...)
	args = append(args, telemetry.KeyValuesFromContext(l.ctx)...)
	args = append(args, keyValuePairs...)

	l.store.mu.Lock()
	l.store.entries = append(l.store.entries, logEntry{
		level: level,
		msg:   msg,
		err:   err,
		kvs:   args,
	})
	l.store.mu.Unlock()
}

func TestWarnMapsToInfo(t *testing.T) {
	spy := newLoggerSpy(telemetry.LevelInfo)
	logger := stdslog.New(NewHandler(spy))

	logger.Warn("warn", stdslog.String("domain", "example.org"))

	entries := spy.entries()
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(entries))
	}
	if entries[0].level != "info" {
		t.Fatalf("expected warn to map to info, got %q", entries[0].level)
	}
}

func TestErrorAttrBecomesErrorArgument(t *testing.T) {
	spy := newLoggerSpy(telemetry.LevelDebug)
	logger := stdslog.New(NewHandler(spy))
	wantErr := errors.New("boom")

	logger.Error("failure", stdslog.Any("error", wantErr), stdslog.String("domain", "example.org"))

	entries := spy.entries()
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(entries))
	}
	entry := entries[0]
	if !errors.Is(entry.err, wantErr) {
		t.Fatalf("expected error argument %v, got %v", wantErr, entry.err)
	}
	wantKVs := []interface{}{"domain", "example.org"}
	if !reflect.DeepEqual(entry.kvs, wantKVs) {
		t.Fatalf("expected kvs %v, got %v", wantKVs, entry.kvs)
	}
}

func TestGroupsAndWithAttrsAreFlattened(t *testing.T) {
	spy := newLoggerSpy(telemetry.LevelDebug)
	logger := stdslog.New(NewHandler(spy)).
		With(stdslog.String("service", "acme")).
		WithGroup("lego")

	logger.Info("start", stdslog.Group("challenge", stdslog.String("domain", "example.org")))

	entries := spy.entries()
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(entries))
	}
	wantKVs := []interface{}{
		"lego.service", "acme",
		"lego.challenge.domain", "example.org",
	}
	if !reflect.DeepEqual(entries[0].kvs, wantKVs) {
		t.Fatalf("expected kvs %v, got %v", wantKVs, entries[0].kvs)
	}
}

func TestDebugRespectsEnabledLevel(t *testing.T) {
	spy := newLoggerSpy(telemetry.LevelInfo)
	logger := stdslog.New(NewHandler(spy))

	logger.Debug("debug-hidden", stdslog.String("k", "v"))

	if got := len(spy.entries()); got != 0 {
		t.Fatalf("expected no entries, got %d", got)
	}
}

func (l *loggerSpy) entries() []logEntry {
	l.store.mu.Lock()
	defer l.store.mu.Unlock()

	out := make([]logEntry, len(l.store.entries))
	copy(out, l.store.entries)
	return out
}

var _ telemetry.Logger = (*loggerSpy)(nil)
