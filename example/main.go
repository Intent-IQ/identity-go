// Command enrichment demonstrates how a host wires identity-go and keeps
// request mutation and fail-open policy outside the library.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	identitycache "github.com/Intent-IQ/identity-go/cache"
	"github.com/Intent-IQ/identity-go/clock"
	"github.com/Intent-IQ/identity-go/enrichment"
	"github.com/Intent-IQ/identity-go/iiqapi/s2s"
	valkeystore "github.com/Intent-IQ/identity-go/integrations/valkey"
	"github.com/prebid/openrtb/v20/openrtb2"
	"gopkg.in/yaml.v3"
)

type config struct {
	PartnerID string               `yaml:"partner_id"`
	TimeoutMS int                  `yaml:"timeout_ms"`
	Endpoint  string               `yaml:"api_endpoint"`
	Cache     identitycache.Config `yaml:"cache"`
	Valkey    valkeystore.Config   `yaml:"valkey"`
}

func main() {
	configPath := flag.String("config", "config.yaml", "path to the example YAML configuration")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	store, err := valkeystore.New(cfg.Valkey)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	identityCache, err := identitycache.New(identitycache.Dependencies{
		Store:  store,
		Clock:  clock.RealClock{},
		Logger: exampleLogger{},
	}, cfg.Cache)
	if err != nil {
		log.Fatal(err)
	}

	resolver, err := enrichment.New(enrichment.Dependencies{
		S2S:     s2s.NewClient(http.DefaultClient),
		Cache:   identityCache,
		Metrics: enrichment.NoopMetrics{},
		Logger:  exampleLogger{},
	}, cfg.Cache.MaxKeys)
	if err != nil {
		log.Fatal(err)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		auction := exampleAuction()
		request := enrichment.Request{
			PartnerID:    cfg.PartnerID,
			Endpoint:     cfg.Endpoint,
			Auction:      auction,
			Timeout:      time.Duration(cfg.TimeoutMS) * time.Millisecond,
			CacheEnabled: cfg.Cache.Enabled,
		}
		result, resolveErr := resolver.Enrich(context.Background(), request)
		if resolveErr != nil {
			// The host owns fail-open behavior: leave the auction unchanged.
			log.Printf("attempt %d: enrichment failed open: %v", attempt, resolveErr)
			continue
		}
		appendEIDs(auction, result.EIDs)
		fmt.Printf("attempt %d: outcome=%s eids=%d abTestUuid=%s tc=%v\n",
			attempt, result.Outcome, len(result.EIDs), result.ABTestUUID,
			terminationCause(result.TerminationCause))
	}
}

func terminationCause(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func exampleAuction() *openrtb2.BidRequest {
	return &openrtb2.BidRequest{
		ID: "example-auction",
		User: &openrtb2.User{EIDs: []openrtb2.EID{{
			Source: "pubcid.org",
			UIDs:   []openrtb2.UID{{ID: "example-pubcid"}},
		}}},
		Device: &openrtb2.Device{IP: "192.0.2.1", UA: "identity-go-example"},
	}
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

func (exampleLogger) Debug(message string) { log.Printf("DEBUG %s", message) }
func (exampleLogger) Warn(message string)  { log.Printf("WARN %s", message) }
func (exampleLogger) Error(message string) { log.Printf("ERROR %s", message) }
