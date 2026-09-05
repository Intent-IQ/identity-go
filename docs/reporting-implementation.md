# Reporting implementation

## Goal

Move reusable impression-report construction and delivery from `prebid-go-module` into `identity-go` without changing observable behavior. Reporting remains separate from identity enrichment.

## Current behavior

The auction-response hook skips reporting when `reports_endpoint == ""` or the bid response is nil. A whitespace-only endpoint is not treated as empty. Otherwise it selects the response currency, defaulting to `USD`, and queues one fire-and-forget GET per bid.

Each report uses the enrichment flow metadata captured earlier: auction ID, reference, IP, user agent, A/B test UUID, and termination cause. The bid response is never mutated.

## Public contracts

The low-level API sends an already-built URL without rebuilding or reordering it:

```go
package reporting

type API interface {
	ReportImpression(context.Context, string) error
}
```

The business component reports exactly one bid synchronously. The host decides whether to call it synchronously or in a goroutine.

```go
package reporting

type Reporter interface {
	Report(context.Context, Request) error
}

type Request struct {
	PartnerID        string
	Endpoint         string
	Timeout          time.Duration
	Bid              openrtb2.Bid
	BidderCode       string
	Currency         string
	AuctionID        string
	Reference        string
	IP               string
	UserAgent        string
	ABTestUUID       string
	TerminationCause *int64
}
```

Construction uses dependency ports:

```go
type Dependencies struct {
	API     iiqreporting.API
	Metrics Metrics
	Logger  logging.Logger
}

func New(Dependencies) (Reporter, error)
```

`API` is required. Nil metrics and logger use no-op implementations.

`Reporter` and the HTTP API client must be safe for concurrent calls because the host may report multiple bids in separate goroutines.

## Reporting metrics

Only the two existing events are required initially:

```go
type Metrics interface {
	ImpressionReported(partnerID string)
	ImpressionError(partnerID string)
}
```

The Prometheus integration implements this interface with the existing names:

- `iiq_identity_impression_reported_total{partner_id}`
- `iiq_identity_impression_error_total{partner_id}`

## Report construction

The URL must preserve an existing endpoint query and append parameters in this exact order. Use `?` when the endpoint contains no `?`; otherwise use `&`, matching the current string-concatenation behavior.

```text
at=45
rtype=1
source=pbsgo
dpi=<partner id>
rdata=<ordered JSON>
```

Values use `url.QueryEscape` with `+` replaced by `%20`.

`rdata` preserves this insertion order:

```text
bidderCode, partnerId, cpm, currency,
originalCpm, originalCurrency,
placementId, biddingPlatformId,
vrref, prebidAuctionId, partnerAuctionId,
abTestUuid, terminationCause, ip, ua
```

`biddingPlatformId` is always `"4"`. Only an exactly empty currency becomes `USD`; whitespace currency is retained. Optional string fields are omitted when whitespace-only, but non-blank values are stored without trimming. `terminationCause`, including zero, is included when non-nil. `originalCpm` is included only for a numeric `bid.ext.origbidcpm`; `originalCurrency` is included only for a non-blank string `bid.ext.origbidcur`. Invalid bid extensions are ignored.

An insertion-ordered encoder is required; a normal map or `url.Values.Encode` must not determine output order.

The existing implementation ignores an ordered-JSON marshal error and sends an empty `rdata` value. Preserve this edge behavior rather than turning it into an additional reporting error.

## API behavior

- An exactly empty endpoint is a successful no-op and emits no reporting metric.
- Send a GET to the exact supplied URL.
- Apply `Request.Timeout` only around the API call.
- Preserve `context.WithTimeout` behavior for zero or negative timeouts; they expire immediately.
- Drain and close every received response body for connection reuse.
- Preserve request, transport, and timeout errors using `iiqapi.Error`.
- Match the existing module by treating any received HTTP response, including non-2xx, as reported.
- Match the existing module by ignoring response-body drain and close errors.
- Emit `ImpressionReported` after any received response; emit `ImpressionError` when request creation or transport fails.
- Log an API failure and emit its metric exactly once inside `Reporter`.
- Return the same error so the host can observe it without duplicating business metrics or ordinary error logging; reporting never changes the auction response.

The preliminary `iiqapi/reporting` client currently builds queries from `url.Values` and rejects non-2xx responses. It must be revised to this contract during implementation.

## Ownership

`identity-go/reporting` owns one-bid payload construction, exact URL construction, default currency, timeout, API invocation, logging, and reporting metrics.

The host adapter owns bid-response nil checks, iteration over seat bids, fire-and-forget goroutines, use of a background context after the hook returns, panic recovery, tracing, configuration mapping, and lifecycle management. It passes enrichment metadata from `enrichment.Result` into each reporting request.

## Compatibility tests

- Empty endpoint is a no-op.
- One report is produced per bid and the bid response is unchanged.
- Missing response currency becomes `USD`.
- Fixed query parameters and existing endpoint queries are preserved.
- `rdata` JSON and URL encoding are exact and deterministic.
- Original CPM/currency parsing covers valid, blank, non-numeric, and malformed extensions.
- All optional enrichment metadata is present or omitted correctly, including termination cause.
- Request and transport failures return errors and increment `ImpressionError`.
- Received 2xx and non-2xx responses are drained and closed, drain/close errors are ignored, and `ImpressionReported` is incremented.
- Multiple reports can run concurrently when the host chooses asynchronous execution.
