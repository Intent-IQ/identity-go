package enrichment

import (
	"github.com/Intent-IQ/identity-go/iiqapi/s2s"
	"github.com/Intent-IQ/identity-go/logging"
)

type Dependencies struct {
	Logger                logging.Logger
	S2S                   s2s.API
	MaxBackgroundS2SCalls int
	Cache                 Cache
	Metrics               Metrics
}
