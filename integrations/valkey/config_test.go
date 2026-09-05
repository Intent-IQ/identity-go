package valkey

import (
	"strings"
	"testing"
	"time"
)

func TestConfigValidationAndTimeout(t *testing.T) {
	if err := (Config{Host: "localhost", Port: 6379}).Validate(); err != nil {
		t.Fatalf("valid config error = %v", err)
	}
	for _, test := range []struct {
		config Config
		field  string
	}{
		{Config{Port: 6379}, "host"},
		{Config{Host: "not a host", Port: 6379}, "host"},
		{Config{Host: "localhost"}, "port"},
		{Config{Host: "localhost", Port: 65536}, "port"},
		{Config{Host: "localhost", Port: 6379, ConnectTimeoutMs: -1}, "connect_timeout_ms"},
	} {
		if err := test.config.Validate(); err == nil || !strings.Contains(err.Error(), test.field) {
			t.Fatalf("Validate(%#v) error = %v, want %q", test.config, err, test.field)
		}
	}
	if got := (Config{}).ConnectTimeout(); got != 5*time.Second {
		t.Fatalf("default timeout = %v", got)
	}
	if got := (Config{ConnectTimeoutMs: 250}).ConnectTimeout(); got != 250*time.Millisecond {
		t.Fatalf("configured timeout = %v", got)
	}
}
