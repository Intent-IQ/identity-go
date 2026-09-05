package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Intent-IQ/identity-go/iiqapi"
	"github.com/Intent-IQ/identity-go/iiqapi/s2s"
	"github.com/prebid/openrtb/v20/openrtb2"
)

type s2sCall struct {
	requestURL  string
	consent     string
	deadline    time.Time
	hasDeadline bool
}

type recordingS2S struct {
	response s2s.Response
	err      error
	resolve  func(context.Context) (s2s.Response, error)
	calls    []s2sCall
}

func (api *recordingS2S) Resolve(ctx context.Context, requestURL, consent string) (s2s.Response, error) {
	deadline, hasDeadline := ctx.Deadline()
	api.calls = append(api.calls, s2sCall{
		requestURL: requestURL, consent: consent, deadline: deadline, hasDeadline: hasDeadline,
	})
	if api.resolve != nil {
		return api.resolve(ctx)
	}
	return api.response, api.err
}

type enrichmentMetricEvent struct {
	name      string
	partnerID string
	reason    string
	kind      string
	status    int
	duration  time.Duration
	lookup    CacheLookupResult
	layer     CacheLayer
}

type recordingEnrichmentMetrics struct{ events []enrichmentMetricEvent }

func (metrics *recordingEnrichmentMetrics) Request(partnerID string) {
	metrics.events = append(metrics.events, enrichmentMetricEvent{name: "request", partnerID: partnerID})
}
func (metrics *recordingEnrichmentMetrics) Enriched(partnerID string) {
	metrics.events = append(metrics.events, enrichmentMetricEvent{name: "enriched", partnerID: partnerID})
}
func (metrics *recordingEnrichmentMetrics) NotEnriched(partnerID string, reason NotEnrichedReason) {
	metrics.events = append(metrics.events, enrichmentMetricEvent{name: "not_enriched", partnerID: partnerID, reason: string(reason)})
}
func (metrics *recordingEnrichmentMetrics) APIRequestDuration(partnerID string, duration time.Duration) {
	metrics.events = append(metrics.events, enrichmentMetricEvent{name: "api_duration", partnerID: partnerID, duration: duration})
}
func (metrics *recordingEnrichmentMetrics) APISuccess(partnerID string) {
	metrics.events = append(metrics.events, enrichmentMetricEvent{name: "api_success", partnerID: partnerID})
}
func (metrics *recordingEnrichmentMetrics) APIError(partnerID, kind string, statusCode int) {
	metrics.events = append(metrics.events, enrichmentMetricEvent{name: "api_error", partnerID: partnerID, kind: kind, status: statusCode})
}
func (metrics *recordingEnrichmentMetrics) CacheLookup(partnerID string, result CacheLookupResult, layer CacheLayer) {
	metrics.events = append(metrics.events, enrichmentMetricEvent{name: "cache_lookup", partnerID: partnerID, lookup: result, layer: layer})
}

type recordingEnrichmentLogger struct{ warnings []string }

func (*recordingEnrichmentLogger) Debug(string) {}
func (*recordingEnrichmentLogger) Error(string) {}
func (logger *recordingEnrichmentLogger) Warn(message string) {
	logger.warnings = append(logger.warnings, message)
}

func newTestEnricher(t *testing.T, api *recordingS2S, metrics *recordingEnrichmentMetrics, logger *recordingEnrichmentLogger) Enricher {
	t.Helper()
	created, err := New(Dependencies{S2S: api, Metrics: metrics, Logger: logger}, 10)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return created
}

func TestNewEnricher(t *testing.T) {
	if _, err := New(Dependencies{}, 10); !errors.Is(err, errS2SRequired) {
		t.Fatalf("New() error = %v, want %v", err, errS2SRequired)
	}
	created, err := New(Dependencies{S2S: &recordingS2S{}}, 7)
	if err != nil {
		t.Fatalf("New() with defaults error = %v", err)
	}
	implementation, ok := created.(*enricher)
	if !ok || implementation.metrics == nil || implementation.logger == nil || implementation.maxCacheKeys != 7 {
		t.Fatalf("New() = %#v, defaults or key limit not retained", created)
	}
}

func TestEnrichMapsRequestAndResponse(t *testing.T) {
	cacheTTL := int64(60)
	terminationCause := int64(120088)
	api := &recordingS2S{response: s2s.Response{
		Data:     json.RawMessage(`{"eids":[{"source":"first.com","uids":[{"id":"one"}]},{"source":"second.com","uids":[{"id":"two"}]}]}`),
		CacheTTL: &cacheTTL, ABTestUUID: "ab-1", TC: &terminationCause,
	}}
	metrics := &recordingEnrichmentMetrics{}
	logger := &recordingEnrichmentLogger{}
	gdpr := int8(1)
	input := Request{
		PartnerID: "partner-42", Endpoint: "https://example.test/resolve", Timeout: time.Second,
		Auction: &openrtb2.BidRequest{
			Device: &openrtb2.Device{IP: "1.2.3.4"},
			Regs:   &openrtb2.Regs{GDPR: &gdpr},
			User:   &openrtb2.User{Consent: "TCF-CONSENT"},
		},
	}

	result, err := newTestEnricher(t, api, metrics, logger).Enrich(t.Context(), input)
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}
	if result.Outcome != OutcomeEnriched || result.CacheTTL != time.Minute || result.ABTestUUID != "ab-1" || result.TerminationCause == nil || *result.TerminationCause != terminationCause {
		t.Fatalf("Enrich() result = %#v", result)
	}
	if got := []string{result.EIDs[0].Source, result.EIDs[1].Source}; !reflect.DeepEqual(got, []string{"first.com", "second.com"}) {
		t.Fatalf("EID order = %v", got)
	}
	if len(api.calls) != 1 {
		t.Fatalf("S2S calls = %d, want 1", len(api.calls))
	}
	wantURL := "https://example.test/resolve?at=39&mi=10&dpi=partner-42&pt=17&dpn=1&srvrReq=true&source=pbsgo&ip=1.2.3.4&gdpr=1"
	if api.calls[0].requestURL != wantURL || api.calls[0].consent != "TCF-CONSENT" {
		t.Fatalf("S2S call = %#v, want URL %q and consent", api.calls[0], wantURL)
	}
	if !api.calls[0].hasDeadline {
		t.Fatal("S2S context has no deadline")
	}
	assertEnrichmentMetricNames(t, metrics.events, "request", "api_duration", "api_success", "enriched")
	assertPartnerIDs(t, metrics.events, "partner-42")
	if len(logger.warnings) != 0 {
		t.Fatalf("warnings = %q, want none", logger.warnings)
	}
}

func TestEnrichNoIDs(t *testing.T) {
	cacheTTL := int64(30)
	terminationCause := int64(7)
	api := &recordingS2S{response: s2s.Response{
		Data: json.RawMessage(`{"eids":[]}`), CacheTTL: &cacheTTL, ABTestUUID: "ab-empty", TC: &terminationCause,
	}}
	metrics := &recordingEnrichmentMetrics{}
	result, err := newTestEnricher(t, api, metrics, &recordingEnrichmentLogger{}).Enrich(t.Context(), Request{
		PartnerID: "partner", Endpoint: "https://example.test", Auction: &openrtb2.BidRequest{}, Timeout: time.Second,
	})
	if err != nil || result.Outcome != OutcomeNoIDs || len(result.EIDs) != 0 || result.CacheTTL != 30*time.Second || result.ABTestUUID != "ab-empty" || result.TerminationCause == nil || *result.TerminationCause != 7 {
		t.Fatalf("Enrich() = (%#v, %v)", result, err)
	}
	assertEnrichmentMetricNames(t, metrics.events, "request", "api_duration", "api_success", "not_enriched")
	if metrics.events[3].reason != string(ReasonNoIDs) {
		t.Fatalf("not-enriched reason = %q", metrics.events[3].reason)
	}
}

func TestEnrichSkipsBlankEndpoint(t *testing.T) {
	api := &recordingS2S{}
	metrics := &recordingEnrichmentMetrics{}
	result, err := newTestEnricher(t, api, metrics, &recordingEnrichmentLogger{}).Enrich(t.Context(), Request{
		PartnerID: "partner", Endpoint: "  ", Auction: &openrtb2.BidRequest{},
	})
	if err != nil || result.Outcome != OutcomeNoEndpoint || len(api.calls) != 0 {
		t.Fatalf("Enrich() = (%#v, %v), S2S calls=%d", result, err, len(api.calls))
	}
	assertEnrichmentMetricNames(t, metrics.events, "request", "not_enriched")
	if metrics.events[1].reason != string(ReasonNoEndpoint) {
		t.Fatalf("reason = %q", metrics.events[1].reason)
	}
}

func TestEnrichSkipsNilAuction(t *testing.T) {
	api := &recordingS2S{}
	metrics := &recordingEnrichmentMetrics{}
	result, err := newTestEnricher(t, api, metrics, &recordingEnrichmentLogger{}).Enrich(t.Context(), Request{
		PartnerID: "partner", Endpoint: "https://example.test", Auction: nil,
	})
	if err != nil || !reflect.DeepEqual(result, Result{}) || len(api.calls) != 0 {
		t.Fatalf("Enrich() = (%#v, %v), S2S calls=%d", result, err, len(api.calls))
	}
	assertEnrichmentMetricNames(t, metrics.events, "request")
}

func TestEnrichReturnsClassifiedS2SErrors(t *testing.T) {
	tests := []struct {
		name   string
		error  error
		kind   string
		status int
	}{
		{"request", &iiqapi.Error{Kind: iiqapi.ErrorRequest, Err: errors.New("bad request")}, "request", 0},
		{"transport", &iiqapi.Error{Kind: iiqapi.ErrorTransport, Err: errors.New("down")}, "transport", 0},
		{"status", &iiqapi.Error{Kind: iiqapi.ErrorStatus, Status: 503, Err: errors.New("unavailable")}, "status", 503},
		{"body read", &iiqapi.Error{Kind: iiqapi.ErrorBodyRead, Status: 200, Err: errors.New("read")}, "body_read", 200},
		{"parse", &iiqapi.Error{Kind: iiqapi.ErrorParse, Status: 200, Err: errors.New("parse")}, "parse", 200},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			api := &recordingS2S{err: test.error}
			metrics := &recordingEnrichmentMetrics{}
			logger := &recordingEnrichmentLogger{}
			result, err := newTestEnricher(t, api, metrics, logger).Enrich(t.Context(), Request{
				PartnerID: "partner", Endpoint: "https://example.test", Auction: &openrtb2.BidRequest{}, Timeout: time.Second,
			})
			if !errors.Is(err, test.error) || !reflect.DeepEqual(result, Result{}) {
				t.Fatalf("Enrich() = (%#v, %v), want original error", result, err)
			}
			assertEnrichmentMetricNames(t, metrics.events, "request", "api_duration", "api_error")
			last := metrics.events[2]
			if last.kind != test.kind || last.status != test.status {
				t.Fatalf("APIError = kind %q status %d", last.kind, last.status)
			}
			if len(logger.warnings) != 1 || !strings.Contains(logger.warnings[0], "kind="+test.kind) {
				t.Fatalf("warnings = %q", logger.warnings)
			}
		})
	}
}

func TestEnrichAppliesTimeoutOnlyToS2SCall(t *testing.T) {
	api := &recordingS2S{resolve: func(ctx context.Context) (s2s.Response, error) {
		<-ctx.Done()
		return s2s.Response{}, ctx.Err()
	}}
	metrics := &recordingEnrichmentMetrics{}
	logger := &recordingEnrichmentLogger{}
	parent := t.Context()
	result, err := newTestEnricher(t, api, metrics, logger).Enrich(parent, Request{
		PartnerID: "partner", Endpoint: "https://example.test", Auction: &openrtb2.BidRequest{}, Timeout: 10 * time.Millisecond,
	})
	if !errors.Is(err, context.DeadlineExceeded) || !reflect.DeepEqual(result, Result{}) {
		t.Fatalf("Enrich() = (%#v, %v), want deadline exceeded", result, err)
	}
	if parent.Err() != nil {
		t.Fatalf("parent context was canceled: %v", parent.Err())
	}
	assertEnrichmentMetricNames(t, metrics.events, "request", "api_duration", "api_error")
	if metrics.events[2].kind != string(iiqapi.ErrorTimeout) || metrics.events[2].status != 0 {
		t.Fatalf("timeout metric = %#v", metrics.events[2])
	}
}

func assertEnrichmentMetricNames(t *testing.T, events []enrichmentMetricEvent, want ...string) {
	t.Helper()
	got := make([]string, len(events))
	for index, event := range events {
		got[index] = event.name
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("metric order = %v, want %v", got, want)
	}
}

func assertPartnerIDs(t *testing.T, events []enrichmentMetricEvent, want string) {
	t.Helper()
	for _, event := range events {
		if event.partnerID != want {
			t.Fatalf("event %q partner = %q, want %q", event.name, event.partnerID, want)
		}
	}
}
