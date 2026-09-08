// Package reporting implements the IIQ impression reporting API.
package reporting

import (
	"context"
)

type API interface {
	ReportImpression(ctx context.Context, requestURL string) error
}
