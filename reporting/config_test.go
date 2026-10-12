package reporting

import (
	"errors"
	"testing"
)

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		want   error
	}{
		{name: "valid", config: Config{MaxConcurrentCalls: 1}},
		{name: "zero", config: Config{}, want: errMaxConcurrentCallsNotPositive},
		{name: "negative", config: Config{MaxConcurrentCalls: -1}, want: errMaxConcurrentCallsNotPositive},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.config.Validate()
			if !errors.Is(err, test.want) {
				t.Fatalf("Validate() error = %v, want %v", err, test.want)
			}
		})
	}
}
