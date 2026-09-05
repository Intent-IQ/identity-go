package redis

import (
	"time"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"
)

const (
	maxPort                 = 65535
	defaultConnectTimeoutMs = 5_000
)

type Config struct {
	Host             string `json:"host"`
	Port             int    `json:"port"`
	Password         string `json:"password"`
	ConnectTimeoutMs int    `json:"connect_timeout_ms"`
}

func (config Config) ConnectTimeout() time.Duration {
	if config.ConnectTimeoutMs <= 0 {
		return defaultConnectTimeoutMs * time.Millisecond
	}
	return time.Duration(config.ConnectTimeoutMs) * time.Millisecond
}

func (config Config) Validate() error {
	return validation.ValidateStruct(&config,
		validation.Field(&config.Host, validation.Required, is.Host),
		validation.Field(&config.Port, validation.Required, validation.Min(1), validation.Max(maxPort)),
		validation.Field(&config.ConnectTimeoutMs, validation.Min(0)),
	)
}
