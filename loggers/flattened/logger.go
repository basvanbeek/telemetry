// Copyright (c) Bas van Beek 2021-2026.
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

// Package flattened provides a flattened logger implementation.
package flattened

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/basvanbeek/telemetry"
	"github.com/basvanbeek/telemetry/function"
)

// Logger is a flattened logger implementation.
type Logger struct {
	telemetry.Logger
	w          io.Writer
	now        func() time.Time
	sourceInfo bool
	multiLine  bool
	strip      func(string) string
}

func stripPathPrefix() func(path string) string {
	// Find project root
	projectRoot, _ := os.Getwd()
	goPath := os.Getenv("GOPATH")
	if goPath == "" {
		homeDir, _ := os.UserHomeDir()
		goPath = homeDir + "/go"
	}

	projectRoot += "/"
	goModCache := goPath + "/pkg/mod/"
	goSrc := goPath + "/src/" // Add this for go.work local modules

	if projectRoot == "//" {
		// process was build in a docker environment
		projectRoot = "/go/src/app/vendor/"
		goModCache = "/go/src/app/"
		goSrc = "/go/src/"
	}

	projectRootLen := len(projectRoot)
	goModCacheLen := len(goModCache)
	goSrcLen := len(goSrc)

	return func(path string) string {
		if strings.HasPrefix(path, projectRoot) {
			return path[projectRootLen:]
		}
		if strings.HasPrefix(path, goSrc) {
			return path[goSrcLen:] // Strip GOPATH/src/ for workspace modules
		}
		if strings.HasPrefix(path, goModCache) {
			return path[goModCacheLen:]
		}
		return path
	}
}

// NewLogger creates a new flattened logger.
func NewLogger(w io.Writer, sourceInfo, multiLine bool) *Logger {
	logger := &Logger{w: w, now: time.Now, sourceInfo: sourceInfo, multiLine: multiLine, strip: stripPathPrefix()}
	logger.Logger = function.NewLogger(logger.Emit, 3)
	return logger
}

// Emit emits a log message.
func (l *Logger) Emit(level telemetry.Level, msg string, err error, values function.Values, callerSkip int) {
	var (
		t   = l.now()
		out bytes.Buffer
	)

	if l.multiLine {
		if l.sourceInfo && callerSkip >= 0 {
			if _, file, line, ok := runtime.Caller(callerSkip); ok {
				_, _ = out.WriteString(fmt.Sprintf("%02d-%02d-%02d %02d:%02d:%02d  %-5v  %s\n    %s",
					t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second(),
					level, l.strip(file)+":"+strconv.Itoa(line), msg),
				)
			} else {
				_, _ = out.WriteString(fmt.Sprintf("%02d-%02d-%02d %02d:%02d:%02d  %-5v  %s",
					t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second(), level, msg))
			}
		}

	} else {
		_, _ = out.WriteString(fmt.Sprintf("%02d-%02d-%02d %02d:%02d:%02d  %-5v  %s",
			t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second(), level, msg))
	}

	nCtx := len(values.FromContext)
	nWith := len(values.FromLogger)
	nMeth := len(values.FromMethod)

	if !l.multiLine && l.sourceInfo && callerSkip >= 0 {
		if _, file, line, ok := runtime.Caller(callerSkip); ok {
			values.FromContext = append(values.FromContext,
				"file", l.strip(file)+":"+strconv.Itoa(line))
			nCtx += 2
		}
	}

	if err != nil || nCtx > 0 || nWith > 0 || nMeth > 0 {
		if l.multiLine {
			_, _ = out.WriteString("\n")
			_, _ = out.WriteString(strings.Repeat(" ", 3))
		}
		if err != nil {
			_, _ = out.WriteString(fmt.Sprintf(" [error=%q", err.Error()))
			if nCtx > 0 || nWith > 0 || nMeth > 0 {
				_, _ = out.WriteString(" ")
			}
		} else {
			_, _ = out.WriteString(" [")
		}

		writeArgs(&out, values.FromContext, values.FromLogger, values.FromMethod)

		_, _ = out.WriteString("]")
	}

	_, _ = out.WriteString("\n")
	_, _ = out.WriteTo(l.w)
}

// writeArgs writes the collection of argument lists to the buffer.
func writeArgs(b *bytes.Buffer, args ...[]any) {
	firstValue := true
	for i := 0; i < len(args); i++ {
		n := len(args[i])
		if n%2 == 1 {
			args[i] = append(args[i], "(MISSING)")
			n++
		}
		for j := 0; j < n; j += 2 {
			if !firstValue {
				_, _ = b.WriteString(" ")
			}
			firstValue = false
			if k, ok := args[i][j].(string); ok {
				_, _ = b.WriteString(k + "=")
			} else {
				_, _ = fmt.Fprintf(b, "%v=", args[i][j])
			}
			if v, ok := args[i][j+1].(string); ok {
				_, _ = fmt.Fprintf(b, "%q", v)
			} else {
				_, _ = fmt.Fprintf(b, "%v", args[i][j+1])
			}
		}
	}
}
