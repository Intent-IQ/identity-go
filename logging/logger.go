// Package logging defines the logger contract used by core components.
package logging

type Logger interface {
	Debug(message string)
	Warn(message string)
	Error(message string)
}

type NoopLogger struct{}

func (NoopLogger) Debug(string) {}
func (NoopLogger) Warn(string)  {}
func (NoopLogger) Error(string) {}

var _ Logger = NoopLogger{}
