package enrichment

import (
	"github.com/Intent-IQ/identity-go/iiqapi/s2s"
	"github.com/Intent-IQ/identity-go/logging"
)

type Dependencies struct {
	S2S     s2s.API
	Cache   Cache
	Metrics Metrics
	Logger  logging.Logger
}
