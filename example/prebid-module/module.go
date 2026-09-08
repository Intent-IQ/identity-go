// Package identitymodule demonstrates a Prebid Server module backed by identity-go.
package identitymodule

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	iiqidcache "github.com/Intent-IQ/identity-go/cache"
	iiqidenrichment "github.com/Intent-IQ/identity-go/enrichment"
	iiqidreportingapi "github.com/Intent-IQ/identity-go/iiqapi/reporting"
	iiqids2s "github.com/Intent-IQ/identity-go/iiqapi/s2s"
	iiqidaerospikestore "github.com/Intent-IQ/identity-go/integrations/aerospike"
	iiqidprometheus "github.com/Intent-IQ/identity-go/integrations/prometheus"
	iiqidredisstore "github.com/Intent-IQ/identity-go/integrations/redis"
	iiqidvalkeystore "github.com/Intent-IQ/identity-go/integrations/valkey"
	iiqidreporting "github.com/Intent-IQ/identity-go/reporting"
	"github.com/prebid/prebid-server/v4/hooks/hookstage"
	"github.com/prebid/prebid-server/v4/modules/moduledeps"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Module struct {
	config        Config
	enricher      iiqidenrichment.Enricher
	reporter      iiqidreporting.Reporter
	store         iiqidcache.Store
	registry      *prometheus.Registry
	metricsServer *http.Server
}

type slogLogger struct{}

var (
	_ hookstage.ProcessedAuctionRequest = (*Module)(nil)
	_ hookstage.AuctionResponse         = (*Module)(nil)
)

func Builder(rawConfig json.RawMessage, dependencies moduledeps.ModuleDeps) (interface{}, error) {
	config := defaultConfig()
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, fmt.Errorf("intentiq identity: parse config: %w", err)
		}
	}

	httpClient := dependencies.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	logger := slogLogger{}

	var registry *prometheus.Registry
	var enrichmentMetrics iiqidenrichment.Metrics = iiqidenrichment.NoopMetrics{}
	var cacheMetrics iiqidcache.Metrics = iiqidcache.NoopMetrics{}
	var reportingMetrics iiqidreporting.Metrics = iiqidreporting.NoopMetrics{}

	if config.MetricsEnabled {
		registry = prometheus.NewRegistry()

		metrics, err := iiqidprometheus.New(registry)
		if err != nil {
			return nil, fmt.Errorf("intentiq identity: create metrics: %w", err)
		}

		enrichmentMetrics = metrics
		cacheMetrics = metrics
		reportingMetrics = metrics
	}

	metricsServer, err := serveMetrics(config.MetricsEnabled, config.MetricsPort, registry)
	if err != nil {
		return nil, fmt.Errorf("intentiq identity: serve metrics: %w", err)
	}

	store, err := newStore(config)
	if err != nil {
		closeMetricsServer(metricsServer)
		return nil, fmt.Errorf("intentiq identity: create cache store: %w", err)
	}

	identityCache, err := iiqidcache.New(iiqidcache.Dependencies{
		Store:   store,
		Metrics: cacheMetrics,
		Logger:  logger,
	}, config.Cache)
	if err != nil {
		if store != nil {
			_ = store.Close()
		}
		closeMetricsServer(metricsServer)
		return nil, fmt.Errorf("intentiq identity: create cache: %w", err)
	}

	enricher, err := iiqidenrichment.New(iiqidenrichment.Dependencies{
		S2S:     iiqids2s.NewClient(httpClient),
		Cache:   identityCache,
		Metrics: enrichmentMetrics,
		Logger:  logger,
	}, config.Cache.MaxKeys)
	if err != nil {
		if store != nil {
			_ = store.Close()
		}
		closeMetricsServer(metricsServer)
		return nil, fmt.Errorf("intentiq identity: create enricher: %w", err)
	}

	reporter, err := iiqidreporting.New(iiqidreporting.Dependencies{
		API:     iiqidreportingapi.NewClient(httpClient),
		Metrics: reportingMetrics,
		Logger:  logger,
	})
	if err != nil {
		if store != nil {
			_ = store.Close()
		}
		closeMetricsServer(metricsServer)
		return nil, fmt.Errorf("intentiq identity: create reporter: %w", err)
	}

	return &Module{
		config:        config,
		enricher:      enricher,
		reporter:      reporter,
		store:         store,
		registry:      registry,
		metricsServer: metricsServer,
	}, nil
}

func (module *Module) MetricsGatherer() prometheus.Gatherer {
	if module.registry == nil {
		return nil
	}

	return module.registry
}

func (module *Module) Shutdown() error {
	closeMetricsServer(module.metricsServer)
	if module.store == nil {
		return nil
	}

	return module.store.Close()
}

func (slogLogger) Debug(message string) { slog.Debug(message) }
func (slogLogger) Warn(message string)  { slog.Warn(message) }
func (slogLogger) Error(message string) { slog.Error(message) }

func serveMetrics(enabled bool, port int, registry *prometheus.Registry) (*http.Server, error) {
	if !enabled || port <= 0 {
		return nil, nil
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics server failed", "error", err)
		}
	}()

	return server, nil
}

func closeMetricsServer(server *http.Server) {
	if server != nil {
		_ = server.Close()
	}
}

func newStore(config Config) (iiqidcache.Store, error) {
	if !config.Cache.Enabled {
		return nil, nil
	}

	switch config.Cache.Provider {
	case "aerospike":
		if config.Aerospike == nil {
			return nil, fmt.Errorf("aerospike configuration is required")
		}

		if err := config.Aerospike.Validate(); err != nil {
			return nil, err
		}

		return iiqidaerospikestore.New(*config.Aerospike)

	case "redis":
		if config.Redis == nil {
			return nil, fmt.Errorf("redis configuration is required")
		}

		if err := config.Redis.Validate(); err != nil {
			return nil, err
		}

		return iiqidredisstore.New(*config.Redis)

	case "valkey":
		if config.Valkey == nil {
			return nil, fmt.Errorf("valkey configuration is required")
		}

		if err := config.Valkey.Validate(); err != nil {
			return nil, err
		}

		return iiqidvalkeystore.New(*config.Valkey)

	default:
		return nil, fmt.Errorf("unknown cache provider %q", config.Cache.Provider)
	}
}
