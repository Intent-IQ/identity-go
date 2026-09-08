// Package s2s implements the IIQ identity-resolution S2S API.
package s2s

import "context"

const GDPRConsentHeader = "gdpr-consent"

// API resolves identities through the IIQ S2S API.
type API interface {
	Resolve(ctx context.Context, requestURL, consent string) (Response, error)
}
