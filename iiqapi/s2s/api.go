// Package s2s implements the IIQ identity-resolution S2S API.
package s2s

import "context"

type API interface {
	Resolve(ctx context.Context, requestURL, consent string) (Response, error)
}
