package logging

import "testing"

func TestNoopLogger(t *testing.T) {
	logger := NoopLogger{}
	logger.Debug("debug")
	logger.Warn("warn")
	logger.Error("error")
}
