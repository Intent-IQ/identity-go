package cache

import (
	"time"

	"github.com/Intent-IQ/identity-go/enrichment"
)

// TTLPolicy applies backend TTLs and identifier-specific compatibility ceilings.
type TTLPolicy struct {
	Default           time.Duration
	FirstPartyCeiling time.Duration
	ThirdPartyCeiling time.Duration
	DeviceCeiling     time.Duration
	NegativeTTL       time.Duration
}

func (policy TTLPolicy) CeilingFor(keyType enrichment.CacheKeyType) time.Duration {
	switch keyType {
	case enrichment.CacheKeyFirstParty:
		return policy.FirstPartyCeiling
	case enrichment.CacheKeyThirdParty:
		return policy.ThirdPartyCeiling
	case enrichment.CacheKeyDevice:
		return policy.DeviceCeiling
	default:
		return policy.Default
	}
}

func (policy TTLPolicy) EffectiveTTL(keyType enrichment.CacheKeyType, cacheTTL time.Duration) time.Duration {
	base := policy.Default
	if cacheTTL > 0 {
		base = cacheTTL
	}
	return min(base, policy.CeilingFor(keyType))
}

// NegativeTTLFor honors a positive backend TTL, capped by the first-party ceiling.
func (policy TTLPolicy) NegativeTTLFor(cacheTTL time.Duration) time.Duration {
	if cacheTTL > 0 {
		return min(cacheTTL, policy.FirstPartyCeiling)
	}
	return policy.NegativeTTL
}
