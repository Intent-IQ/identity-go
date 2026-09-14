// Package cache defines the generic identity cache dependencies and configuration.
package cache

import (
	"context"
	"time"
)

type Store interface {
	Get(context.Context, string) ([]byte, error)
	Put(context.Context, string, []byte, time.Duration) error
	Close() error
}
