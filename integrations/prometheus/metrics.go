package prometheus

import (
	"fmt"
	"strconv"
	"time"

	identitycache "github.com/Intent-IQ/identity-go/cache"
	"github.com/Intent-IQ/identity-go/enrichment"
	prom "github.com/prometheus/client_golang/prometheus"
)

const (
	Namespace = "iiq"
	Subsystem = "identity"
	Prefix    = Namespace + "_" + Subsystem + "_"
)

var l2LatencyBuckets = []float64{.0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1}

// Metrics implements both enrichment.Metrics and cache.Metrics. Collector
// registration is idempotent for a shared registry, so hosts can safely reuse
// an integration during component wiring.
type Metrics struct {
	cacheLookup *prom.CounterVec
	requests    *prom.CounterVec
	apiSuccess  *prom.CounterVec
	apiError    *prom.CounterVec
	enriched    *prom.CounterVec
	notEnriched *prom.CounterVec
	apiLatency  *prom.HistogramVec

	l2GetLatency prom.Histogram
	l2PutLatency prom.Histogram
	l2Requests   *prom.CounterVec
}

// New registers the identity collectors with registerer. A nil registerer uses
// Prometheus's default registerer. Existing compatible collectors are reused.
func New(registerer prom.Registerer) (*Metrics, error) {
	if registerer == nil {
		registerer = prom.DefaultRegisterer
	}

	counterOpts := func(name, help string) prom.CounterOpts {
		return prom.CounterOpts{Namespace: Namespace, Subsystem: Subsystem, Name: name, Help: help}
	}
	histogramOpts := func(name, help string, buckets []float64) prom.HistogramOpts {
		return prom.HistogramOpts{Namespace: Namespace, Subsystem: Subsystem, Name: name, Help: help, Buckets: buckets}
	}

	metrics := &Metrics{}
	var err error
	if metrics.cacheLookup, err = registerCounterVec(registerer, prom.NewCounterVec(counterOpts(
		"cache_lookup_total",
		"Cache lookups by result: hit=positive entry served (no API call), miss=no positive entry (true miss, negative sentinel or in-flight marker). layer is l1/l2 on a hit, none on a miss. Sum = total lookups.",
	), []string{"result", "layer", "partner_id"})); err != nil {
		return nil, err
	}
	if metrics.requests, err = registerCounterVec(registerer, prom.NewCounterVec(counterOpts(
		"requests_total", "Enrich-hook invocations (identity ingress QPS), by partner_id.",
	), []string{"partner_id"})); err != nil {
		return nil, err
	}
	if metrics.apiSuccess, err = registerCounterVec(registerer, prom.NewCounterVec(counterOpts(
		"api_success_total", "Resolution API responded 2xx and parsed OK, by partner_id.",
	), []string{"partner_id"})); err != nil {
		return nil, err
	}
	if metrics.apiError, err = registerCounterVec(registerer, prom.NewCounterVec(counterOpts(
		"api_error_total",
		"Resolution S2S failures, by reason (timeout|transport|status|body_read|parse|request), status_code (HTTP code when a response was received, else empty), and partner_id.",
	), []string{"reason", "status_code", "partner_id"})); err != nil {
		return nil, err
	}
	if metrics.enriched, err = registerCounterVec(registerer, prom.NewCounterVec(counterOpts(
		"enriched_total", "Resolutions that added >=1 eid to user.eids (a match), by partner_id.",
	), []string{"partner_id"})); err != nil {
		return nil, err
	}
	if metrics.notEnriched, err = registerCounterVec(registerer, prom.NewCounterVec(counterOpts(
		"not_enriched_total",
		"Resolutions that added no eids, by reason (no_ids|no_ids_cached|in_progress|no_endpoint) and partner_id. S2S failures are counted in api_error_total.",
	), []string{"reason", "partner_id"})); err != nil {
		return nil, err
	}
	if metrics.apiLatency, err = registerHistogramVec(registerer, prom.NewHistogramVec(histogramOpts(
		"api_latency_seconds", "Resolution API call duration in seconds, by partner_id.", prom.DefBuckets,
	), []string{"partner_id"})); err != nil {
		return nil, err
	}
	if metrics.l2GetLatency, err = registerHistogram(registerer, prom.NewHistogram(histogramOpts(
		"l2_get_latency_seconds", "L2 (shared store) GET duration in seconds.", l2LatencyBuckets,
	))); err != nil {
		return nil, err
	}
	if metrics.l2PutLatency, err = registerHistogram(registerer, prom.NewHistogram(histogramOpts(
		"l2_put_latency_seconds", "L2 (shared store) PUT duration in seconds.", l2LatencyBuckets,
	))); err != nil {
		return nil, err
	}
	if metrics.l2Requests, err = registerCounterVec(registerer, prom.NewCounterVec(counterOpts(
		"l2_requests_total", "L2 (shared store) operations, by op (get|put) and result (hit|miss|stored|error).",
	), []string{"op", "result"})); err != nil {
		return nil, err
	}
	return metrics, nil
}

func (metrics *Metrics) Request(partnerID string) {
	metrics.requests.WithLabelValues(partnerID).Inc()
}

func (metrics *Metrics) Enriched(partnerID string) {
	metrics.enriched.WithLabelValues(partnerID).Inc()
}

func (metrics *Metrics) NotEnriched(partnerID string, reason enrichment.NotEnrichedReason) {
	metrics.notEnriched.WithLabelValues(string(reason), partnerID).Inc()
}

func (metrics *Metrics) APIRequestDuration(partnerID string, duration time.Duration) {
	metrics.apiLatency.WithLabelValues(partnerID).Observe(duration.Seconds())
}

func (metrics *Metrics) APISuccess(partnerID string) {
	metrics.apiSuccess.WithLabelValues(partnerID).Inc()
}

func (metrics *Metrics) APIError(partnerID, kind string, statusCode int) {
	status := ""
	if statusCode != 0 {
		status = strconv.Itoa(statusCode)
	}
	metrics.apiError.WithLabelValues(kind, status, partnerID).Inc()
}

func (metrics *Metrics) CacheLookup(partnerID string, result enrichment.CacheLookupResult, layer enrichment.CacheLayer) {
	metrics.cacheLookup.WithLabelValues(string(result), layer.Token(), partnerID).Inc()
}

func (metrics *Metrics) L2Request(operation, result string) {
	metrics.l2Requests.WithLabelValues(operation, result).Inc()
}

func (metrics *Metrics) L2GetLatency(duration time.Duration) {
	metrics.l2GetLatency.Observe(duration.Seconds())
}

func (metrics *Metrics) L2PutLatency(duration time.Duration) {
	metrics.l2PutLatency.Observe(duration.Seconds())
}

func registerCounterVec(registerer prom.Registerer, collector *prom.CounterVec) (*prom.CounterVec, error) {
	existing, err := register(registerer, collector)
	if err != nil {
		return nil, err
	}
	registered, ok := existing.(*prom.CounterVec)
	if !ok {
		return nil, fmt.Errorf("registered collector has type %T, want *prometheus.CounterVec", existing)
	}
	return registered, nil
}

func registerHistogramVec(registerer prom.Registerer, collector *prom.HistogramVec) (*prom.HistogramVec, error) {
	existing, err := register(registerer, collector)
	if err != nil {
		return nil, err
	}
	registered, ok := existing.(*prom.HistogramVec)
	if !ok {
		return nil, fmt.Errorf("registered collector has type %T, want *prometheus.HistogramVec", existing)
	}
	return registered, nil
}

func registerHistogram(registerer prom.Registerer, collector prom.Histogram) (prom.Histogram, error) {
	existing, err := register(registerer, collector)
	if err != nil {
		return nil, err
	}
	registered, ok := existing.(prom.Histogram)
	if !ok {
		return nil, fmt.Errorf("registered collector has type %T, want prometheus.Histogram", existing)
	}
	return registered, nil
}

func register(registerer prom.Registerer, collector prom.Collector) (prom.Collector, error) {
	if err := registerer.Register(collector); err != nil {
		alreadyRegistered, ok := err.(prom.AlreadyRegisteredError)
		if !ok {
			return nil, err
		}
		return alreadyRegistered.ExistingCollector, nil
	}
	return collector, nil
}

var _ enrichment.Metrics = (*Metrics)(nil)
var _ identitycache.Metrics = (*Metrics)(nil)
