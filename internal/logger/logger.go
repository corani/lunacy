// Package logger provides a minimal structured logger writing to stderr.
package logger

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Logger struct {
	prefix string
}

func New(prefix string) *Logger {
	return &Logger{prefix: prefix}
}

func (l *Logger) Info(msg string, args ...any) {
	l.log("INFO", msg, args...)
}

func (l *Logger) Error(msg string, args ...any) {
	l.log("ERR ", msg, args...)
}

func (l *Logger) log(level, msg string, args ...any) {
	ts := time.Now().Format("15:04:05.000")
	var line strings.Builder
	fmt.Fprintf(&line, "%s %s [%s] %s", ts, level, l.prefix, msg)
	for i := 0; i+1 < len(args); i += 2 {
		fmt.Fprintf(&line, "  %v=%v", args[i], args[i+1])
	}
	fmt.Fprintln(os.Stderr, line.String())
}
