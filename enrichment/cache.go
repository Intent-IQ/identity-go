package enrichment

import (
	"context"
	"time"
)

type Cache interface {
	Get(context.Context, []CacheKey) (CacheResult, error)
	PutResolved(context.Context, []CacheKey, Result) error
	PutNegative(context.Context, []CacheKey, ResultMetadata) error
	PutInProgress(context.Context, []CacheKey, time.Duration) error
}

type CacheState int

const (
	CacheMiss CacheState = iota
	CacheHit
	CacheNegative
	CacheInProgress
)

func (state CacheState) Token() string {
	switch state {
	case CacheMiss:
		return "miss"
	case CacheHit:
		return "hit"
	case CacheNegative:
		return "negative"
	case CacheInProgress:
		return "in_progress"
	default:
		return "unknown"
	}
}

type CacheLayer int

const (
	CacheLayerNone CacheLayer = iota
	CacheLayerL1
	CacheLayerL2
)

func (layer CacheLayer) Token() string {
	switch layer {
	case CacheLayerL1:
		return "l1"
	case CacheLayerL2:
		return "l2"
	default:
		return "none"
	}
}

type CacheKeyType int

const (
	CacheKeyFirstParty CacheKeyType = iota
	CacheKeyThirdParty
	CacheKeyDevice
)

func (keyType CacheKeyType) Token() string {
	switch keyType {
	case CacheKeyFirstParty:
		return "first_party"
	case CacheKeyThirdParty:
		return "third_party"
	case CacheKeyDevice:
		return "device"
	default:
		return "unknown"
	}
}

type CacheKey struct {
	Value string
	Type  CacheKeyType
}

type CacheResult struct {
	State   CacheState
	Layer   CacheLayer
	KeyType CacheKeyType
	Result  Result
}

type ResultMetadata struct {
	CacheTTL         time.Duration
	ABTestUUID       string
	TerminationCause *int64
}
