// Package reporting implements the IIQ impression reporting API.
package reporting

import (
	"context"
	"net/url"
)

type API interface {
	ReportImpression(context.Context, Request) error
}

type Request struct {
	Endpoint string
	Params   url.Values
}
