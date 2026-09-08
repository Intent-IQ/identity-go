// Command example demonstrates host wiring for enrichment and reporting.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"sync"
	"time"

	identitycache "github.com/Intent-IQ/identity-go/cache"
	"github.com/Intent-IQ/identity-go/clock"
	"github.com/Intent-IQ/identity-go/enrichment"
	reportingapi "github.com/Intent-IQ/identity-go/iiqapi/reporting"
	"github.com/Intent-IQ/identity-go/iiqapi/s2s"
	valkeystore "github.com/Intent-IQ/identity-go/integrations/valkey"
	identityreporting "github.com/Intent-IQ/identity-go/reporting"
	"github.com/prebid/openrtb/v20/openrtb2"
	"gopkg.in/yaml.v3"
)

type config struct {
	PartnerID       string               `yaml:"partner_id"`
	TimeoutMS       int                  `yaml:"timeout_ms"`
	Endpoint        string               `yaml:"api_endpoint"`
	ReportsEndpoint string               `yaml:"reports_endpoint"`
	Cache           identitycache.Config `yaml:"cache"`
	Valkey          valkeystore.Config   `yaml:"valkey"`
}

func main() {
	if err := run(); err != nil {
		slog.Error("example failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "config.yaml", "path to the example YAML configuration")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}

	// Build these components once during host startup and reuse them across auctions.
	enricher, cleanupEnrichment, err := setupEnrichment(cfg)
	if err != nil {
		return err
	}
	defer cleanupEnrichment()

	reporter, err := setupReporting()
	if err != nil {
		return err
	}

	// The host passes an auction to the Enricher, applies returned EIDs, and owns fail-open behavior.
	enrichedAuction, enrichmentResult := runEnrichmentExample(enricher, cfg)

	// At auction-response time, the host maps each bid and starts reporting asynchronously.
	bidResponse := exampleBidResponse()
	reports := reportBidResponse(reporter, cfg, enrichedAuction, enrichmentResult, bidResponse)
	slog.Info("impression reports queued", "count", reports.count, "bid_response_mutated", false)

	// A long-running server does not wait here; this example waits before process shutdown.
	reports.Wait()
	return nil
}

func runEnrichmentExample(enricher enrichment.Enricher, cfg config) (*openrtb2.BidRequest, enrichment.Result) {
	var auction *openrtb2.BidRequest
	var result enrichment.Result
	for attempt := 1; attempt <= 2; attempt++ {
		auction = exampleAuction()
		request := enrichment.Request{
			PartnerID:    cfg.PartnerID,
			Endpoint:     cfg.Endpoint,
			Auction:      auction,
			Timeout:      time.Duration(cfg.TimeoutMS) * time.Millisecond,
			CacheEnabled: cfg.Cache.Enabled,
		}
		var enrichErr error
		result, enrichErr = enricher.Enrich(context.Background(), request)
		if enrichErr != nil {
			// The host owns fail-open behavior: leave the auction unchanged.
			slog.Warn("enrichment failed open", "attempt", attempt, "error", enrichErr)
			result = enrichment.Result{}
			continue
		}
		appendEIDs(auction, result.EIDs)
		slog.Info("enrichment completed",
			"attempt", attempt,
			"outcome", result.Outcome,
			"eids", len(result.EIDs),
			"ab_test_uuid", result.ABTestUUID,
			"termination_cause", terminationCause(result.TerminationCause),
		)
	}
	return auction, result
}

func setupEnrichment(cfg config) (enrichment.Enricher, func(), error) {
	store, err := valkeystore.New(cfg.Valkey)
	if err != nil {
		return nil, nil, fmt.Errorf("create Valkey store: %w", err)
	}
	cleanup := func() { _ = store.Close() }

	identityCache, err := identitycache.New(identitycache.Dependencies{
		Store:  store,
		Clock:  clock.RealClock{},
		Logger: exampleLogger{},
	}, cfg.Cache)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("create identity cache: %w", err)
	}

	enricher, err := enrichment.New(enrichment.Dependencies{
		S2S:     s2s.NewClient(http.DefaultClient),
		Cache:   identityCache,
		Metrics: enrichment.NoopMetrics{},
		Logger:  exampleLogger{},
	}, cfg.Cache.MaxKeys)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("create enricher: %w", err)
	}
	return enricher, cleanup, nil
}

func setupReporting() (identityreporting.Reporter, error) {
	reporter, err := identityreporting.New(identityreporting.Dependencies{
		API:     reportingapi.NewClient(http.DefaultClient),
		Metrics: identityreporting.NoopMetrics{},
		Logger:  exampleLogger{},
	})
	if err != nil {
		return nil, fmt.Errorf("create reporter: %w", err)
	}
	return reporter, nil
}

type pendingReports struct {
	sync.WaitGroup
	count int
}

// reportBidResponse demonstrates the adapter-owned portion of the flow. It only
// reads the bid response and starts one detached report per bid. A real server
// can return its auction response immediately instead of waiting as this short-
// lived command does before process exit.
func reportBidResponse(
	reporter identityreporting.Reporter,
	cfg config,
	auction *openrtb2.BidRequest,
	result enrichment.Result,
	bidResponse *openrtb2.BidResponse,
) *pendingReports {
	pending := &pendingReports{}
	if cfg.ReportsEndpoint == "" || bidResponse == nil {
		return pending
	}

	currency := bidResponse.Cur
	for seatIndex := range bidResponse.SeatBid {
		seatBid := bidResponse.SeatBid[seatIndex]
		for bidIndex := range seatBid.Bid {
			request := identityreporting.Request{
				PartnerID:        cfg.PartnerID,
				Endpoint:         cfg.ReportsEndpoint,
				Timeout:          time.Duration(cfg.TimeoutMS) * time.Millisecond,
				Bid:              seatBid.Bid[bidIndex],
				BidderCode:       seatBid.Seat,
				Currency:         currency,
				AuctionID:        auction.ID,
				Reference:        auctionReference(auction),
				IP:               auctionIP(auction),
				UserAgent:        auctionUserAgent(auction),
				ABTestUUID:       result.ABTestUUID,
				TerminationCause: result.TerminationCause,
			}
			pending.Add(1)
			pending.count++
			go func() {
				defer pending.Done()
				defer func() {
					if recovered := recover(); recovered != nil {
						slog.Error("panic in impression report",
							"partner_id", cfg.PartnerID,
							"panic", recovered,
							"stack", string(debug.Stack()),
						)
					}
				}()
				_ = reporter.Report(context.Background(), request)
			}()
		}
	}
	return pending
}

func terminationCause(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func exampleAuction() *openrtb2.BidRequest {
	return &openrtb2.BidRequest{
		ID:   "example-auction",
		Site: &openrtb2.Site{Domain: "example.com"},
		User: &openrtb2.User{EIDs: []openrtb2.EID{{
			Source: "pubcid.org",
			UIDs:   []openrtb2.UID{{ID: "example-pubcid"}},
		}}},
		Device: &openrtb2.Device{IP: "192.0.2.1", UA: "identity-go-example"},
	}
}

func exampleBidResponse() *openrtb2.BidResponse {
	return &openrtb2.BidResponse{
		Cur: "EUR",
		SeatBid: []openrtb2.SeatBid{{
			Seat: "example-bidder",
			Bid: []openrtb2.Bid{{
				ImpID: "example-imp",
				Price: 1.5,
				Ext:   json.RawMessage(`{"origbidcpm":1.7,"origbidcur":"USD"}`),
			}},
		}},
	}
}

func auctionReference(auction *openrtb2.BidRequest) string {
	if auction.Site != nil {
		if auction.Site.Domain != "" {
			return auction.Site.Domain
		}
		return auction.Site.Page
	}
	if auction.App != nil {
		if auction.App.Bundle != "" {
			return auction.App.Bundle
		}
		return auction.App.Name
	}
	return ""
}

func auctionIP(auction *openrtb2.BidRequest) string {
	if auction.Device == nil {
		return ""
	}
	if auction.Device.IP != "" {
		return auction.Device.IP
	}
	return auction.Device.IPv6
}

func auctionUserAgent(auction *openrtb2.BidRequest) string {
	if auction.Device == nil {
		return ""
	}
	return auction.Device.UA
}

func loadConfig(path string) (config, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return config{}, fmt.Errorf("read config: %w", err)
	}
	var result config
	if err := yaml.Unmarshal(contents, &result); err != nil {
		return config{}, fmt.Errorf("parse config: %w", err)
	}
	return result, nil
}

func appendEIDs(auction *openrtb2.BidRequest, resolved []openrtb2.EID) {
	if len(resolved) == 0 {
		return
	}
	if auction.User == nil {
		auction.User = &openrtb2.User{}
	}
	auction.User.EIDs = append(auction.User.EIDs, resolved...)
}

type exampleLogger struct{}

func (exampleLogger) Debug(message string) { slog.Debug(message) }
func (exampleLogger) Warn(message string)  { slog.Warn(message) }
func (exampleLogger) Error(message string) { slog.Error(message) }
