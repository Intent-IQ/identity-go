package reporting

import (
	"errors"
	"fmt"
)

var errMaxConcurrentCallsNotPositive = errors.New("reporting max concurrent calls must be positive")

type Config struct {
	MaxConcurrentCalls int `json:"max_concurrent_calls" yaml:"max_concurrent_calls"`
}

func (config Config) Validate() error {
	if config.MaxConcurrentCalls <= 0 {
		return fmt.Errorf("%w: %d", errMaxConcurrentCallsNotPositive, config.MaxConcurrentCalls)
	}
	return nil
}
