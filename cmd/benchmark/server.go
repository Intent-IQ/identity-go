package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	identitycache "github.com/Intent-IQ/identity-go/cache"
	"github.com/Intent-IQ/identity-go/clock"
	"github.com/Intent-IQ/identity-go/enrichment"
	"github.com/Intent-IQ/identity-go/iiqapi"
	"github.com/Intent-IQ/identity-go/iiqapi/s2s"
	iiqprometheus "github.com/Intent-IQ/identity-go/integrations/prometheus"
	"github.com/prebid/openrtb/v20/openrtb2"
	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type serverConfig struct {
	listen             string
	endpoint           string
	partnerID          string
	timeout            time.Duration
	concurrency        int
	maxConcurrentCalls int
	verbose            bool
	cacheDir           string
	cacheTTL           time.Duration
	cacheMaxKeys       int
}

func main() {
	if err := run(); err != nil {
		slog.Error("benchmark server failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := parseFlags()
	if err := cfg.validate(); err != nil {
		return err
	}

	registry := prom.NewRegistry()
	metrics, err := iiqprometheus.New(registry)
	if err != nil {
		return fmt.Errorf("create metrics: %w", err)
	}
	enricher, err := buildEnricher(cfg, metrics, metrics)
	if err != nil {
		return err
	}
	return serve(cfg, enricher, registry)
}

func parseFlags() serverConfig {
	cfg := serverConfig{}
	flag.StringVar(&cfg.listen, "listen", ":8081", "address to serve POST /enrich, GET /health and GET /metrics on")
	flag.StringVar(&cfg.endpoint, "endpoint", "", "IIQ S2S endpoint (required)")
	flag.StringVar(&cfg.partnerID, "partner", "", "IIQ partner ID (required)")
	flag.DurationVar(&cfg.timeout, "timeout", 400*time.Millisecond, "per-request S2S timeout")
	flag.IntVar(&cfg.concurrency, "concurrency", 16, "expected concurrent callers; sizes the idle connection pool")
	flag.IntVar(&cfg.maxConcurrentCalls, "max-concurrent-calls", 2000, "maximum concurrent S2S calls in async or hybrid mode")
	flag.BoolVar(&cfg.verbose, "verbose", false, "log enrichment warnings")
	flag.StringVar(&cfg.cacheDir, "cache", "", "enable the identity cache, persisting entries in this directory; required for the async and hybrid modes")
	flag.DurationVar(&cfg.cacheTTL, "cache-ttl", time.Hour, "cache TTL used when the API returns none")
	flag.IntVar(&cfg.cacheMaxKeys, "cache-max-keys", 10, "maximum cache aliases per auction")
	flag.Parse()
	return cfg
}

func (cfg *serverConfig) validate() error {
	if cfg.endpoint == "" || cfg.partnerID == "" {
		return errors.New("-endpoint and -partner are required")
	}
	if cfg.concurrency < 1 {
		return errors.New("-concurrency must be positive")
	}
	if cfg.maxConcurrentCalls < 1 {
		return errors.New("-max-concurrent-calls must be positive")
	}
	return nil
}

func buildEnricher(cfg serverConfig, metrics enrichment.Metrics, cacheMetrics identitycache.Metrics) (enrichment.Enricher, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Avoid the default two-idle-connections-per-host bottleneck during a run.
	transport.MaxIdleConns = cfg.concurrency * 2
	transport.MaxIdleConnsPerHost = cfg.concurrency * 2

	dependencies := enrichment.Dependencies{
		S2S:                s2s.NewClient(&http.Client{Transport: transport}),
		Metrics:            metrics,
		MaxConcurrentCalls: cfg.maxConcurrentCalls,
	}
	if cfg.verbose {
		dependencies.Logger = slogLogger{}
	}

	if cfg.cacheDir == "" {
		return enrichment.New(dependencies, 0)
	}

	store, err := newFileStore(cfg.cacheDir)
	if err != nil {
		return nil, fmt.Errorf("create cache store: %w", err)
	}
	identityCache, err := identitycache.New(identitycache.Dependencies{
		Store:   store,
		Clock:   clock.RealClock{},
		Metrics: cacheMetrics,
	}, benchmarkCacheConfig(cfg))
	if err != nil {
		return nil, fmt.Errorf("create identity cache: %w", err)
	}
	dependencies.Cache = identityCache
	return enrichment.New(dependencies, cfg.cacheMaxKeys)
}

func benchmarkCacheConfig(cfg serverConfig) identitycache.Config {
	return identitycache.Config{
		Enabled:                     true,
		Provider:                    "file",
		TTLSeconds:                  int(cfg.cacheTTL.Seconds()),
		MaxKeys:                     cfg.cacheMaxKeys,
		MaxSize:                     1 << 20,
		TTLCeilingFirstPartySeconds: 86400,
		TTLCeilingThirdPartySeconds: 43200,
		TTLCeilingDeviceSeconds:     900,
		NegativeTTLSeconds:          300,
	}
}

// Prebid Server promotes user.ext.eids before invoking modules.
func liftExtEIDs(auction *openrtb2.BidRequest) {
	user := auction.User
	if user == nil || len(user.EIDs) > 0 || len(user.Ext) == 0 {
		return
	}
	var extension struct {
		EIDs []openrtb2.EID `json:"eids"`
	}
	if json.Unmarshal(user.Ext, &extension) != nil || len(extension.EIDs) == 0 {
		return
	}
	user.EIDs = extension.EIDs
}

func reference(auction *openrtb2.BidRequest) string {
	if site := auction.Site; site != nil {
		if site.Domain != "" {
			return site.Domain
		}
		return site.Page
	}
	if app := auction.App; app != nil {
		if app.Bundle != "" {
			return app.Bundle
		}
		return app.Name
	}
	return ""
}

func classify(err error) (kind string, status int) {
	kind, _ = iiqapi.ErrorLabels(err)
	var apiError *iiqapi.Error
	if errors.As(err, &apiError) {
		status = apiError.Status
	}
	return kind, status
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}

type record struct {
	AuctionID  string        `json:"auction_id,omitempty"`
	Reference  string        `json:"reference,omitempty"`
	DurationMS float64       `json:"duration_ms"`
	Outcome    string        `json:"outcome,omitempty"`
	FromCache  bool          `json:"served_from_cache,omitempty"`
	Result     *resultRecord `json:"enrichment_result,omitempty"`
	Error      *errorRecord  `json:"error,omitempty"`
}

type resultRecord struct {
	EIDs             []openrtb2.EID `json:"eids"`
	CacheTTLMS       int64          `json:"cache_ttl_ms"`
	ABTestUUID       string         `json:"ab_test_uuid,omitempty"`
	TerminationCause *int64         `json:"termination_cause,omitempty"`
}

type errorRecord struct {
	Kind    string `json:"kind"`
	Status  int    `json:"status,omitempty"`
	Message string `json:"message"`
}

type slogLogger struct{}

func (slogLogger) Debug(message string) { slog.Debug(message) }
func (slogLogger) Warn(message string)  { slog.Warn(message) }
func (slogLogger) Error(message string) { slog.Error(message) }

const maxBodySize = 16 << 20

func serve(cfg serverConfig, enricher enrichment.Enricher, registry *prom.Registry) error {
	mux := http.NewServeMux()
	mux.Handle("POST /enrich", benchmarkHandler(cfg, enricher))
	mux.HandleFunc("GET /health", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = writer.Write([]byte("ok\n"))
	})
	mux.Handle("GET /metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	server := &http.Server{Addr: cfg.listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	slog.Info("benchmark server listening",
		"addr", cfg.listen, "timeout", cfg.timeout, "cache", cfg.cacheDir != "", "max_concurrent_calls", cfg.maxConcurrentCalls)
	return server.ListenAndServe()
}

func benchmarkHandler(cfg serverConfig, enricher enrichment.Enricher) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		wait, err := requestWait(cfg, request.URL.Query())
		if err != nil {
			respond(writer, http.StatusBadRequest, record{
				Outcome: "bad_request",
				Error:   &errorRecord{Kind: "request_mode", Message: err.Error()},
			})
			return
		}

		var auction openrtb2.BidRequest
		if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxBodySize)).Decode(&auction); err != nil {
			respond(writer, http.StatusBadRequest, record{
				Outcome: "bad_request",
				Error:   &errorRecord{Kind: "request_parse", Message: err.Error()},
			})
			return
		}
		liftExtEIDs(&auction)

		result := record{AuctionID: auction.ID, Reference: reference(&auction)}
		started := time.Now()
		enriched, err := enricher.Enrich(request.Context(), enrichment.Request{
			PartnerID:    cfg.partnerID,
			Endpoint:     cfg.endpoint,
			Auction:      &auction,
			Timeout:      cfg.timeout,
			WaitTimeout:  wait,
			CacheEnabled: cfg.cacheDir != "",
		})
		result.DurationMS = milliseconds(time.Since(started))
		if err != nil {
			kind, status := classify(err)
			result.Outcome = "error"
			result.Error = &errorRecord{Kind: kind, Status: status, Message: err.Error()}
			respond(writer, http.StatusOK, result)
			return
		}

		result.Outcome = string(enriched.Outcome)
		result.FromCache = enriched.FromCache()
		result.Result = &resultRecord{
			EIDs:             enriched.EIDs,
			CacheTTLMS:       enriched.CacheTTL.Milliseconds(),
			ABTestUUID:       enriched.ABTestUUID,
			TerminationCause: enriched.TerminationCause,
		}
		respond(writer, http.StatusOK, result)
	})
}

func requestWait(cfg serverConfig, query url.Values) (*time.Duration, error) {
	mode := query.Get("mode")
	if mode == "" {
		mode = "sync"
	}
	if mode != "sync" && cfg.cacheDir == "" {
		return nil, fmt.Errorf("mode %s requires -cache", mode)
	}

	switch mode {
	case "sync":
		return nil, nil
	case "async":
		zero := time.Duration(0)
		return &zero, nil
	case "hybrid":
		milliseconds, err := strconv.Atoi(query.Get("wait_ms"))
		if err != nil || milliseconds <= 0 {
			return nil, fmt.Errorf("hybrid mode needs a positive wait_ms, got %q", query.Get("wait_ms"))
		}
		wait := time.Duration(milliseconds) * time.Millisecond
		if wait >= cfg.timeout {
			return nil, fmt.Errorf("wait_ms %d must be less than timeout %v", milliseconds, cfg.timeout)
		}
		return &wait, nil
	default:
		return nil, fmt.Errorf("unknown mode %q; want sync, async or hybrid", mode)
	}
}

func respond(writer http.ResponseWriter, status int, body record) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}
