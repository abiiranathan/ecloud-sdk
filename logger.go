package ecloudsdk

import (
	"fmt"
	"io"
)

// NoOpLogger is a default logger that does nothing
type NoOpLogger struct{}

func (l *NoOpLogger) Debug(msg string, args ...any) {}
func (l *NoOpLogger) Info(msg string, args ...any)  {}
func (l *NoOpLogger) Error(msg string, args ...any) {}

type StdLogger struct {
	out io.Writer
}

func NewLogger(out io.Writer) *StdLogger {
	return &StdLogger{out}
}

func (l *StdLogger) Debug(msg string, args ...any) {
	fmt.Fprintf(l.out, "[DEBUG]: "+msg, args...)
}

func (l *StdLogger) Info(msg string, args ...any) {
	fmt.Fprintf(l.out, "[INFO]: "+msg, args...)
}

func (l *StdLogger) Error(msg string, args ...any) {
	fmt.Fprintf(l.out, "[ERROR]: "+msg, args...)
}
