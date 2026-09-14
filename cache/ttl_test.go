package cache

import (
	"testing"
	"time"

	"github.com/Intent-IQ/identity-go/enrichment"
)

func TestConfigTTLPolicy(t *testing.T) {
	policy := (Config{
		TTLSeconds:                  100,
		TTLCeilingFirstPartySeconds: 200,
		TTLCeilingThirdPartySeconds: 300,
		TTLCeilingDeviceSeconds:     400,
		NegativeTTLSeconds:          5,
	}).TTLPolicy()

	assertDuration(t, "default", 100*time.Second, policy.Default)
	assertDuration(t, "first-party ceiling", 200*time.Second, policy.FirstPartyCeiling)
	assertDuration(t, "third-party ceiling", 300*time.Second, policy.ThirdPartyCeiling)
	assertDuration(t, "device ceiling", 400*time.Second, policy.DeviceCeiling)
	assertDuration(t, "negative", 5*time.Second, policy.NegativeTTL)
}

func TestTTLPolicyCeilingFor(t *testing.T) {
	policy := TTLPolicy{
		Default: 1 * time.Second, FirstPartyCeiling: 2 * time.Second,
		ThirdPartyCeiling: 3 * time.Second, DeviceCeiling: 4 * time.Second,
	}
	tests := []struct {
		keyType enrichment.CacheKeyType
		want    time.Duration
	}{
		{enrichment.CacheKeyFirstParty, 2 * time.Second},
		{enrichment.CacheKeyThirdParty, 3 * time.Second},
		{enrichment.CacheKeyDevice, 4 * time.Second},
		{enrichment.CacheKeyType(99), time.Second},
	}
	for _, test := range tests {
		assertDuration(t, test.keyType.Token(), test.want, policy.CeilingFor(test.keyType))
	}
}

func TestTTLPolicyEffectiveTTL(t *testing.T) {
	policy := TTLPolicy{Default: 10 * time.Second, FirstPartyCeiling: 20 * time.Second}
	tests := []struct {
		name     string
		cacheTTL time.Duration
		want     time.Duration
	}{
		{"default below ceiling", 0, 10 * time.Second},
		{"backend below ceiling", 15 * time.Second, 15 * time.Second},
		{"backend capped", 30 * time.Second, 20 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertDuration(t, test.name, test.want, policy.EffectiveTTL(enrichment.CacheKeyFirstParty, test.cacheTTL))
		})
	}
}

func TestTTLPolicyNegativeTTL(t *testing.T) {
	policy := TTLPolicy{NegativeTTL: 5 * time.Second, FirstPartyCeiling: 20 * time.Second}
	tests := []struct {
		name     string
		cacheTTL time.Duration
		want     time.Duration
	}{
		{"configured", 0, 5 * time.Second},
		{"backend", 15 * time.Second, 15 * time.Second},
		{"backend capped", 30 * time.Second, 20 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertDuration(t, test.name, test.want, policy.NegativeTTLFor(test.cacheTTL))
		})
	}
}

func assertDuration(t *testing.T, name string, want, got time.Duration) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}
