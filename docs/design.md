# identity-go design

## Goal

`identity-go` provides host-independent IntentIQ identity enrichment and impression reporting for `iiq-prebid-server` and official Go Prebid Server.

The library must not import Prebid Server hook packages. It may use `context`, `net/http`, and `github.com/prebid/openrtb/v20/openrtb2`.

## Interfaces

Business interfaces live in the use-case packages:

```go
type enrichment.Enricher interface {
	Enrich(context.Context, enrichment.Request) (enrichment.Result, error)
}

type reporting.Reporter interface {
	Report(context.Context, reporting.Request) error
}
```

IIQ wire-level interfaces live with their API clients:

```go
type s2s.API interface {
	ResolveIdentity(context.Context, s2s.Request) (s2s.Response, error)
}

type reportingapi.API interface {
	ReportImpression(context.Context, reportingapi.Request) error
}
```

Business implementations translate OpenRTB data into API requests. API clients only build/send HTTP requests and parse responses. Calls are synchronous and honor the supplied context. Host adapters own timeouts, asynchronous scheduling, caching, metrics, request mutation, and fail-open policy.

## Folder structure

```text
identity-go/
  go.mod
  docs/design.md
  enrichment/
    enrichment.go       # Enricher and business models
  reporting/
    reporting.go        # Reporter and business models
  iiqapi/
    error.go            # shared typed API errors
    s2s/
      client.go         # identity-resolution API and HTTP client
    reporting/
      client.go         # impression API and HTTP client
```

Prebid-specific adapters remain in each server repository. Shared business implementations can be added to `enrichment` and `reporting` after both adapters validate the contracts.
