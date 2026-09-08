package identitymodule

import (
	"net/http"
	"os"
	"testing"

	iiqidcache "github.com/Intent-IQ/identity-go/cache"
	"github.com/prebid/prebid-server/v4/hooks/hookstage"
	"github.com/prebid/prebid-server/v4/modules/moduledeps"
	dto "github.com/prometheus/client_model/go"
	"gopkg.in/yaml.v3"
)

func TestBuilder(t *testing.T) {
	built, err := Builder([]byte(`{"partner_id":"partner","timeout":250}`), moduledeps.ModuleDeps{HTTPClient: http.DefaultClient})
	if err != nil {
		t.Fatalf("Builder() error = %v", err)
	}
	module, ok := built.(*Module)
	if !ok || module.enricher == nil || module.reporter == nil || module.config.PartnerID != "partner" || module.config.Timeout != 250 {
		t.Fatalf("Builder() = %#v", built)
	}
	if module.MetricsGatherer() == nil {
		t.Fatal("MetricsGatherer() = nil")
	}
	if _, err := module.HandleProcessedAuctionHook(t.Context(), hookstage.ModuleInvocationContext{}, hookstage.ProcessedAuctionRequestPayload{}); err != nil {
		t.Fatalf("processed-auction hook: %v", err)
	}
	families, err := module.MetricsGatherer().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	if !containsMetric(families, "iiq_identity_requests_total") || !containsMetric(families, "iiq_identity_not_enriched_total") {
		t.Fatalf("expected enrichment collectors, got %d metric families", len(families))
	}
	if err := module.Shutdown(); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestBuilderWithMetricsDisabledHasNoGatherer(t *testing.T) {
	built, err := Builder([]byte(`{"metrics_enabled":false}`), moduledeps.ModuleDeps{HTTPClient: http.DefaultClient})
	if err != nil {
		t.Fatalf("Builder() error = %v", err)
	}
	module := built.(*Module)
	if module.MetricsGatherer() != nil {
		t.Fatal("MetricsGatherer() is non-nil with metrics disabled")
	}
}

func TestStoreConfigurationValidation(t *testing.T) {
	if store, err := newStore(Config{}); err != nil || store != nil {
		t.Fatalf("disabled cache returned store=%v err=%v", store, err)
	}
	for _, config := range []Config{
		{Cache: iiqidcache.Config{Enabled: true, Provider: "redis"}},
		{Cache: iiqidcache.Config{Enabled: true, Provider: "valkey"}},
		{Cache: iiqidcache.Config{Enabled: true, Provider: "aerospike"}},
		{Cache: iiqidcache.Config{Enabled: true, Provider: "unknown"}},
	} {
		if _, err := newStore(config); err == nil {
			t.Fatalf("provider %q accepted without valid configuration", config.Cache.Provider)
		}
	}
}

func TestExampleYAMLIsValid(t *testing.T) {
	contents, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(contents, &document); err != nil {
		t.Fatalf("parse config.yaml: %v", err)
	}
	if _, ok := document["hooks"]; !ok {
		t.Fatal("config.yaml has no hooks section")
	}
}

func containsMetric(families []*dto.MetricFamily, name string) bool {
	for _, family := range families {
		if family.GetName() == name {
			return true
		}
	}
	return false
}
