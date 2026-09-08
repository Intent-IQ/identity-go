package reporting

import (
	iiqreporting "github.com/Intent-IQ/identity-go/iiqapi/reporting"
	"github.com/Intent-IQ/identity-go/logging"
)

type Dependencies struct {
	API     iiqreporting.API
	Metrics Metrics
	Logger  logging.Logger
}
