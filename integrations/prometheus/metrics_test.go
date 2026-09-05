package prometheus

import (
	"reflect"
	"sort"
	"testing"
	"time"

	identitycache "github.com/Intent-IQ/identity-go/cache"
	"github.com/Intent-IQ/identity-go/enrichment"
	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)

func newTestMetrics(t *testing.T) (*Metrics, *prom.Registry) {
	t.Helper()
	registry := prom.NewRegistry()
	metrics, err := New(registry)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return metrics, registry
}

func TestEnrichmentMetrics(t *testing.T) {
	metrics, _ := newTestMetrics(t)

	metrics.Request("partner")
	metrics.APISuccess("partner")
	metrics.APIError("partner", "timeout", 0)
	metrics.APIError("partner", "status", 503)
	metrics.Enriched("partner")
	metrics.NotEnriched("partner", enrichment.ReasonNoIDs)
	metrics.NotEnriched("partner", enrichment.ReasonNoIDsCached)
	metrics.NotEnriched("partner", enrichment.ReasonInProgress)
	metrics.NotEnriched("partner", enrichment.ReasonNoEndpoint)
	metrics.CacheLookup("partner", enrichment.CacheLookupHit, enrichment.CacheLayerL1)
	metrics.CacheLookup("partner", enrichment.CacheLookupHit, enrichment.CacheLayerL2)
	metrics.CacheLookup("partner", enrichment.CacheLookupMiss, enrichment.CacheLayerNone)

	assertCounter(t, metrics.requests.WithLabelValues("partner"), 1)
	assertCounter(t, metrics.apiSuccess.WithLabelValues("partner"), 1)
	assertCounter(t, metrics.apiError.WithLabelValues("timeout", "", "partner"), 1)
	assertCounter(t, metrics.apiError.WithLabelValues("status", "503", "partner"), 1)
	assertCounter(t, metrics.enriched.WithLabelValues("partner"), 1)
	assertCounter(t, metrics.notEnriched.WithLabelValues(string(enrichment.ReasonNoIDs), "partner"), 1)
	assertCounter(t, metrics.notEnriched.WithLabelValues(string(enrichment.ReasonNoIDsCached), "partner"), 1)
	assertCounter(t, metrics.notEnriched.WithLabelValues(string(enrichment.ReasonInProgress), "partner"), 1)
	assertCounter(t, metrics.notEnriched.WithLabelValues(string(enrichment.ReasonNoEndpoint), "partner"), 1)
	assertCounter(t, metrics.cacheLookup.WithLabelValues("hit", "l1", "partner"), 1)
	assertCounter(t, metrics.cacheLookup.WithLabelValues("hit", "l2", "partner"), 1)
	assertCounter(t, metrics.cacheLookup.WithLabelValues("miss", "none", "partner"), 1)
}

func TestCacheMetrics(t *testing.T) {
	metrics, _ := newTestMetrics(t)

	metrics.L2Request(identitycache.OperationGet, identitycache.ResultHit)
	metrics.L2Request(identitycache.OperationGet, identitycache.ResultMiss)
	metrics.L2Request(identitycache.OperationGet, identitycache.ResultError)
	metrics.L2Request(identitycache.OperationPut, identitycache.ResultStored)
	metrics.L2Request(identitycache.OperationPut, identitycache.ResultError)
	metrics.L2GetLatency(500 * time.Microsecond)
	metrics.L2PutLatency(time.Millisecond)

	assertCounter(t, metrics.l2Requests.WithLabelValues("get", "hit"), 1)
	assertCounter(t, metrics.l2Requests.WithLabelValues("get", "miss"), 1)
	assertCounter(t, metrics.l2Requests.WithLabelValues("get", "error"), 1)
	assertCounter(t, metrics.l2Requests.WithLabelValues("put", "stored"), 1)
	assertCounter(t, metrics.l2Requests.WithLabelValues("put", "error"), 1)
	if got := testutil.CollectAndCount(metrics.l2GetLatency); got != 1 {
		t.Fatalf("L2 get histogram count = %d", got)
	}
	if got := testutil.CollectAndCount(metrics.l2PutLatency); got != 1 {
		t.Fatalf("L2 put histogram count = %d", got)
	}
}

func TestLatencyObservationsAndMetricNames(t *testing.T) {
	metrics, registry := newTestMetrics(t)
	metrics.Request("partner")
	metrics.APISuccess("partner")
	metrics.APIError("partner", "timeout", 0)
	metrics.Enriched("partner")
	metrics.NotEnriched("partner", enrichment.ReasonNoIDs)
	metrics.CacheLookup("partner", enrichment.CacheLookupMiss, enrichment.CacheLayerNone)
	metrics.APIRequestDuration("partner", 150*time.Millisecond)
	metrics.L2GetLatency(500 * time.Microsecond)
	metrics.L2PutLatency(time.Millisecond)
	metrics.L2Request("get", "hit")

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	names := make([]string, 0, len(families))
	for _, family := range families {
		names = append(names, family.GetName())
		if len(family.GetName()) < len(Prefix) || family.GetName()[:len(Prefix)] != Prefix {
			t.Fatalf("metric %q does not use prefix %q", family.GetName(), Prefix)
		}
	}
	sort.Strings(names)
	want := []string{
		"iiq_identity_api_error_total",
		"iiq_identity_api_latency_seconds",
		"iiq_identity_api_success_total",
		"iiq_identity_cache_lookup_total",
		"iiq_identity_enriched_total",
		"iiq_identity_l2_get_latency_seconds",
		"iiq_identity_l2_put_latency_seconds",
		"iiq_identity_l2_requests_total",
		"iiq_identity_not_enriched_total",
		"iiq_identity_requests_total",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("metric names = %q, want %q", names, want)
	}
	assertHistogram(t, families, "iiq_identity_api_latency_seconds", 1, .15)
	assertHistogram(t, families, "iiq_identity_l2_get_latency_seconds", 1, .0005)
	assertHistogram(t, families, "iiq_identity_l2_put_latency_seconds", 1, .001)
	for _, removed := range []string{"iiq_identity_l1_put_error_total", "iiq_identity_l1_size", "iiq_identity_l1_eviction"} {
		if contains(names, removed) {
			t.Fatalf("removed L1 metric %q was registered", removed)
		}
	}
}

func TestNewReusesCollectorsWithoutDuplicateRegistration(t *testing.T) {
	registry := prom.NewRegistry()
	first, err := New(registry)
	if err != nil {
		t.Fatalf("first New() error = %v", err)
	}
	second, err := New(registry)
	if err != nil {
		t.Fatalf("second New() error = %v", err)
	}
	if first.requests != second.requests || first.l2GetLatency != second.l2GetLatency {
		t.Fatal("New() did not reuse already registered collectors")
	}
	first.Request("partner")
	second.Request("partner")
	assertCounter(t, first.requests.WithLabelValues("partner"), 2)
	if _, err := registry.Gather(); err != nil {
		t.Fatalf("Gather() after repeated construction error = %v", err)
	}
}

func assertCounter(t *testing.T, collector prom.Counter, want float64) {
	t.Helper()
	if got := testutil.ToFloat64(collector); got != want {
		t.Fatalf("counter = %v, want %v", got, want)
	}
}

func assertHistogram(t *testing.T, families []*dto.MetricFamily, name string, wantCount uint64, wantSum float64) {
	t.Helper()
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		if len(family.Metric) != 1 {
			t.Fatalf("histogram %q series = %d, want 1", name, len(family.Metric))
		}
		histogram := family.Metric[0].GetHistogram()
		if histogram.GetSampleCount() != wantCount || histogram.GetSampleSum() != wantSum {
			t.Fatalf("histogram %q = count %d sum %v, want count %d sum %v", name, histogram.GetSampleCount(), histogram.GetSampleSum(), wantCount, wantSum)
		}
		return
	}
	t.Fatalf("histogram %q not found", name)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
