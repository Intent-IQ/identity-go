package enrichment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Intent-IQ/identity-go/iiqapi"
	"github.com/Intent-IQ/identity-go/iiqapi/s2s"
	"github.com/Intent-IQ/identity-go/logging"
)

var errS2SRequired = errors.New("S2S API is required")

type enricher struct {
	s2s          s2s.API
	cache        Cache
	metrics      Metrics
	logger       logging.Logger
	maxCacheKeys int
}

func New(dependencies Dependencies, maxCacheKeys int) (Enricher, error) {
	if dependencies.S2S == nil {
		return nil, errS2SRequired
	}
	if dependencies.Metrics == nil {
		dependencies.Metrics = NoopMetrics{}
	}
	if dependencies.Logger == nil {
		dependencies.Logger = logging.NoopLogger{}
	}
	return &enricher{
		s2s:          dependencies.S2S,
		cache:        dependencies.Cache,
		metrics:      dependencies.Metrics,
		logger:       dependencies.Logger,
		maxCacheKeys: maxCacheKeys,
	}, nil
}

func (enricher *enricher) Enrich(ctx context.Context, input Request) (Result, error) {
	enricher.metrics.Request(input.PartnerID)

	if !notBlank(input.Endpoint) {
		enricher.metrics.NotEnriched(input.PartnerID, ReasonNoEndpoint)
		return Result{Outcome: OutcomeNoEndpoint}, nil
	}
	if input.Auction == nil {
		return Result{}, nil
	}

	if enricher.cache == nil || !input.CacheEnabled {
		return enricher.resolve(ctx, input)
	}

	keys := extractCacheKeys(input.Auction, enricher.maxCacheKeys)
	if len(keys) == 0 {
		return enricher.resolve(ctx, input)
	}

	cached, err := enricher.cache.Get(ctx, keys)
	if err != nil {
		enricher.logger.Warn(fmt.Sprintf("identity enrichment cache read failed: %v", err))
		cached = CacheResult{State: CacheMiss, Layer: CacheLayerNone}
	}

	switch cached.State {
	case CacheHit:
		enricher.metrics.CacheLookup(input.PartnerID, CacheLookupHit, cached.Layer)
		result := cached.Result
		result.Outcome = OutcomeEnriched
		enricher.metrics.Enriched(input.PartnerID)
		return result, nil
	case CacheNegative:
		enricher.metrics.CacheLookup(input.PartnerID, CacheLookupMiss, cached.Layer)
		result := cached.Result
		result.EIDs = nil
		result.Outcome = OutcomeCachedNoIDs
		enricher.metrics.NotEnriched(input.PartnerID, ReasonNoIDsCached)
		return result, nil
	case CacheInProgress:
		enricher.metrics.CacheLookup(input.PartnerID, CacheLookupMiss, cached.Layer)
		enricher.metrics.NotEnriched(input.PartnerID, ReasonInProgress)
		return Result{Outcome: OutcomeInProgress}, nil
	default:
		enricher.metrics.CacheLookup(input.PartnerID, CacheLookupMiss, cached.Layer)
		if err := enricher.cache.PutInProgress(ctx, keys); err != nil {
			enricher.logger.Warn(fmt.Sprintf("identity enrichment cache in-progress write failed: %v", err))
		}
	}

	result, err := enricher.resolve(ctx, input)
	if err != nil {
		return Result{}, err
	}
	if len(result.EIDs) > 0 {
		if err := enricher.cache.PutResolved(ctx, keys, result); err != nil {
			enricher.logger.Warn(fmt.Sprintf("identity enrichment cache resolved write failed: %v", err))
		}
		return result, nil
	}

	metadata := ResultMetadata{
		CacheTTL:         result.CacheTTL,
		ABTestUUID:       result.ABTestUUID,
		TerminationCause: result.TerminationCause,
	}
	if err := enricher.cache.PutNegative(ctx, keys, metadata); err != nil {
		enricher.logger.Warn(fmt.Sprintf("identity enrichment cache negative write failed: %v", err))
	}
	return result, nil
}

func (enricher *enricher) resolve(ctx context.Context, input Request) (Result, error) {
	requestURL, consent := buildS2SRequest(input)
	requestContext, cancel := context.WithTimeout(ctx, input.Timeout)
	started := time.Now()
	response, err := enricher.s2s.Resolve(requestContext, requestURL, consent)
	duration := time.Since(started)
	cancel()

	enricher.metrics.APIRequestDuration(input.PartnerID, duration)
	if err != nil {
		kind, status := classifyS2SError(err)
		enricher.logger.Warn(fmt.Sprintf(
			"identity enrichment S2S request failed: kind=%s status=%d: %v",
			kind, status, err,
		))
		enricher.metrics.APIError(input.PartnerID, kind, status)
		return Result{}, err
	}

	enricher.metrics.APISuccess(input.PartnerID)
	result := Result{
		EIDs:             response.EIDs(),
		CacheTTL:         response.TTL(),
		ABTestUUID:       response.ABTestUUID,
		TerminationCause: response.TC,
	}
	if len(result.EIDs) == 0 {
		result.Outcome = OutcomeNoIDs
		enricher.metrics.NotEnriched(input.PartnerID, ReasonNoIDs)
		return result, nil
	}

	result.Outcome = OutcomeEnriched
	enricher.metrics.Enriched(input.PartnerID)
	return result, nil
}

func classifyS2SError(err error) (kind string, status int) {
	kind, _ = iiqapi.ErrorLabels(err)
	var apiError *iiqapi.Error
	if errors.As(err, &apiError) {
		status = apiError.Status
	}
	return kind, status
}

var _ Enricher = (*enricher)(nil)
