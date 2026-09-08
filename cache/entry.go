package cache

import (
	"encoding/json"
	"time"

	"github.com/Intent-IQ/identity-go/clock"
	"github.com/Intent-IQ/identity-go/enrichment"
	"github.com/prebid/openrtb/v20/openrtb2"
)

// entry is the cache wire model. Its field order and JSON tags are compatibility-sensitive.
type entry struct {
	EIDs             []openrtb2.EID `json:"eids,omitempty"`
	ABTestUUID       string         `json:"abTestUuid,omitempty"`
	TerminationCause *int64         `json:"tc,omitempty"`
	Negative         bool           `json:"negative,omitempty"`
	InProgress       bool           `json:"inProgress,omitempty"`
	ExpiresAt        int64          `json:"exp"`
}

type entryCodec struct {
	clock clock.Clock
}

func newEntryCodec(source clock.Clock) entryCodec {
	if source == nil {
		source = clock.RealClock{}
	}
	return entryCodec{clock: source}
}

func (codec entryCodec) resolved(result enrichment.Result, ttl time.Duration) entry {
	return entry{
		EIDs:             result.EIDs,
		ABTestUUID:       result.ABTestUUID,
		TerminationCause: result.TerminationCause,
		ExpiresAt:        codec.expiry(ttl),
	}
}

func (codec entryCodec) negative(metadata enrichment.ResultMetadata, ttl time.Duration) entry {
	return entry{
		ABTestUUID:       metadata.ABTestUUID,
		TerminationCause: metadata.TerminationCause,
		Negative:         true,
		ExpiresAt:        codec.expiry(ttl),
	}
}

func (codec entryCodec) inProgress(ttl time.Duration) entry {
	return entry{InProgress: true, ExpiresAt: codec.expiry(ttl)}
}

func (codec entryCodec) expiry(ttl time.Duration) int64 {
	return codec.clock.Now().UnixMilli() + ttl.Milliseconds()
}

func (entryCodec) encode(value entry) ([]byte, error) {
	return json.Marshal(value)
}

// decode returns false for empty, malformed, or expired entries.
func (codec entryCodec) decode(value []byte) (entry, bool) {
	if len(value) == 0 {
		return entry{}, false
	}
	var decoded entry
	if err := json.Unmarshal(value, &decoded); err != nil {
		return entry{}, false
	}
	if decoded.ExpiresAt <= codec.clock.Now().UnixMilli() {
		return entry{}, false
	}
	return decoded, true
}

func cacheResultFromEntry(value entry, keyType enrichment.CacheKeyType, layer enrichment.CacheLayer) enrichment.CacheResult {
	result := enrichment.CacheResult{KeyType: keyType, Layer: layer}
	if value.InProgress {
		result.State = enrichment.CacheInProgress
		return result
	}

	result.Result.ABTestUUID = value.ABTestUUID
	result.Result.TerminationCause = value.TerminationCause
	if value.Negative {
		result.State = enrichment.CacheNegative
		return result
	}
	result.State = enrichment.CacheHit
	result.Result.EIDs = value.EIDs
	return result
}
