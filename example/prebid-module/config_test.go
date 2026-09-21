package identitymodule

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	iiqidcache "github.com/Intent-IQ/identity-go/cache"
	"gopkg.in/yaml.v3"
)

func int64Pointer(value int64) *int64 { return &value }

func TestWaitTimeoutPreservesAbsentAndExplicitZero(t *testing.T) {
	if got := (Config{}).waitTimeout(); got != nil {
		t.Fatalf("absent wait timeout = %v, want nil", *got)
	}
	zero := int64(0)
	got := (Config{WaitTimeout: &zero}).waitTimeout()
	if got == nil || *got != 0 {
		t.Fatalf("explicit zero wait timeout = %v", got)
	}
	above := int64(2_000)
	got = (Config{Timeout: 1_000, WaitTimeout: &above}).waitTimeout()
	if got == nil || *got != 2*time.Second {
		t.Fatalf("unclamped wait timeout = %v, want 2s for library normalization", got)
	}
}

func TestConfigValidation(t *testing.T) {
	validCache := iiqidcache.Config{Enabled: true}
	tests := []struct {
		name    string
		config  Config
		wantErr string
	}{
		{name: "sync default", config: Config{Timeout: 1_000}},
		{name: "sync equal without cache", config: Config{Timeout: 1_000, WaitTimeout: int64Pointer(1_000)}},
		{name: "sync above without cache", config: Config{Timeout: 1_000, WaitTimeout: int64Pointer(2_000)}},
		{name: "async with cache", config: Config{Timeout: 1_000, WaitTimeout: int64Pointer(0), Cache: validCache, MaxBackgroundCalls: 10}},
		{name: "hybrid with cache", config: Config{Timeout: 1_000, WaitTimeout: int64Pointer(500), Cache: validCache}},
		{name: "zero timeout", config: Config{}, wantErr: "timeout must be positive"},
		{name: "negative timeout", config: Config{Timeout: -1}, wantErr: "timeout must be positive"},
		{name: "negative wait", config: Config{Timeout: 1_000, WaitTimeout: int64Pointer(-1)}, wantErr: "wait_timeout must not be negative"},
		{name: "negative capacity", config: Config{Timeout: 1_000, MaxBackgroundCalls: -1}, wantErr: "max_background_calls must not be negative"},
		{name: "async without cache", config: Config{Timeout: 1_000, WaitTimeout: int64Pointer(0)}, wantErr: "cache must be enabled"},
		{name: "hybrid without cache", config: Config{Timeout: 1_000, WaitTimeout: int64Pointer(500)}, wantErr: "cache must be enabled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.config.validate()
			if test.wantErr == "" && err != nil {
				t.Fatalf("validate() error = %v", err)
			}
			if test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
				t.Fatalf("validate() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestConfigJSONAndYAMLPreserveWaitTimeout(t *testing.T) {
	for _, test := range []struct {
		name   string
		decode func(*Config) error
	}{
		{
			name: "json",
			decode: func(config *Config) error {
				return json.Unmarshal([]byte(`{"timeout":1000,"wait_timeout":0,"max_background_calls":12}`), config)
			},
		},
		{
			name: "yaml",
			decode: func(config *Config) error {
				return yaml.Unmarshal([]byte("timeout: 1000\nwait_timeout: 0\nmax_background_calls: 12\n"), config)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var config Config
			if err := test.decode(&config); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if config.WaitTimeout == nil || *config.WaitTimeout != 0 || config.MaxBackgroundCalls != 12 {
				t.Fatalf("decoded config = %#v", config)
			}
		})
	}
}

func TestAccountOverlayCannotResizeModuleLimiter(t *testing.T) {
	base := Config{Timeout: 1_000, MaxBackgroundCalls: 25, Cache: iiqidcache.Config{Enabled: true}}
	resolved := base.resolve(json.RawMessage(`{"wait_timeout":0,"max_background_calls":999}`))
	if resolved.WaitTimeout == nil || *resolved.WaitTimeout != 0 {
		t.Fatalf("account wait timeout = %v, want explicit zero", resolved.WaitTimeout)
	}
	if resolved.MaxBackgroundCalls != 25 {
		t.Fatalf("account changed module capacity to %d", resolved.MaxBackgroundCalls)
	}
}

func TestInvalidAccountOverlayFallsBackToModuleConfig(t *testing.T) {
	base := Config{Timeout: 1_000, MaxBackgroundCalls: 25}
	resolved := base.resolve(json.RawMessage(`{"wait_timeout":0}`))
	if resolved.WaitTimeout != nil || resolved.Timeout != base.Timeout {
		t.Fatalf("invalid account overlay was applied: %#v", resolved)
	}
}
