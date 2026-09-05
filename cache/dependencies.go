package cache

import (
	"github.com/Intent-IQ/identity-go/clock"
	"github.com/Intent-IQ/identity-go/logging"
)

type Dependencies struct {
	Store   Store
	Metrics Metrics
	Logger  logging.Logger
	Clock   clock.Clock
}
