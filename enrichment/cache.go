package enrichment

import (
	"context"
	"time"
)

type Cache interface {
	Get(context.Context, []CacheKey) (CacheResult, error)
	PutResolved(context.Context, []CacheKey, Result) error
	PutNegative(context.Context, []CacheKey, ResultMetadata) error
	PutInProgress(context.Context, []CacheKey) error
}

type CacheState int

const (
	CacheMiss CacheState = iota
	CacheHit
	CacheNegative
	CacheInProgress
)

type CacheLayer int

const (
	CacheLayerNone CacheLayer = iota
	CacheLayerL1
	CacheLayerL2
)

type CacheKeyType int

const (
	CacheKeyFirstParty CacheKeyType = iota
	CacheKeyThirdParty
	CacheKeyDevice
)

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
