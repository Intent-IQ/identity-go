package aerospike

import (
	"strings"
	"testing"
)

func validConfig() Config {
	return Config{Host: "localhost", Port: 3000, Namespace: "prebid", Set: "identity"}
}

func TestConfigValidation(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("valid config error = %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*Config)
		field  string
	}{
		{"host required", func(config *Config) { config.Host = "" }, "host"},
		{"host malformed", func(config *Config) { config.Host = "not a host" }, "host"},
		{"port required", func(config *Config) { config.Port = 0 }, "port"},
		{"port bounded", func(config *Config) { config.Port = 65536 }, "port"},
		{"namespace required", func(config *Config) { config.Namespace = "" }, "namespace"},
		{"set required", func(config *Config) { config.Set = "" }, "set"},
		{"policy nonnegative", func(config *Config) { config.Policy.ConnectTimeoutMs = -1 }, "client_policy"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validConfig()
			test.mutate(&config)
			if err := config.Validate(); err == nil || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("Validate() error = %v, want field %q", err, test.field)
			}
		})
	}
}
