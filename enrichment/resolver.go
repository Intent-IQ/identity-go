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

var (
	errS2SRequired                = errors.New("S2S API is required")
	errNegativeBackgroundS2SLimit = errors.New("max background S2S calls must not be negative")
	errTimeoutNotPositive         = errors.New("timeout must be positive")
	errNegativeWaitTimeout        = errors.New("wait timeout must not be negative")
	errNonSyncCacheRequired       = errors.New("cache must be enabled for async or hybrid enrichment")
	errNonSyncCapacityRequired    = errors.New("background S2S capacity must be positive for async or hybrid enrichment")
)

type enricher struct {
	logger       logging.Logger
	s2s          s2s.API
	limiter      s2sLimiter
	cache        Cache
	maxCacheKeys int
	metrics      Metrics
}

// preparedResolution contains everything execution needs after the host request
// is no longer safe to retain. In particular, it contains no auction pointer.
type preparedResolution struct {
	partnerID  string
	requestURL string
	consent    string
	timeout    time.Duration
	cacheKeys  []CacheKey
}

type resolutionCompletion struct {
	result Result
	err    error
}

func New(dependencies Dependencies, maxCacheKeys int) (Enricher, error) {
	if dependencies.S2S == nil {
		return nil, errS2SRequired
	}
	if dependencies.MaxBackgroundS2SCalls < 0 {
		return nil, errNegativeBackgroundS2SLimit
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
		limiter:      newS2SLimiter(dependencies.MaxBackgroundS2SCalls),
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
	if err := enricher.validateRequest(input); err != nil {
		return Result{}, err
	}

	if enricher.cache == nil || !input.CacheEnabled {
		return enricher.executeWithAdmission(ctx, input, nil)
	}

	keys := extractCacheKeys(input.Auction, enricher.maxCacheKeys)
	if len(keys) == 0 {
		return enricher.executeWithAdmission(ctx, input, nil)
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
		result.CacheLayer = cached.Layer
		enricher.metrics.Enriched(input.PartnerID)
		return result, nil
	case CacheNegative:
		enricher.metrics.CacheLookup(input.PartnerID, CacheLookupMiss, cached.Layer)
		result := cached.Result
		result.EIDs = nil
		result.Outcome = OutcomeCachedNoIDs
		result.CacheLayer = cached.Layer
		enricher.metrics.NotEnriched(input.PartnerID, ReasonNoIDsCached)
		return result, nil
	case CacheInProgress:
		enricher.metrics.CacheLookup(input.PartnerID, CacheLookupMiss, cached.Layer)
		enricher.metrics.NotEnriched(input.PartnerID, ReasonInProgress)
		return Result{Outcome: OutcomeInProgress, CacheLayer: cached.Layer}, nil
	default:
		enricher.metrics.CacheLookup(input.PartnerID, CacheLookupMiss, cached.Layer)
		release, admitted := enricher.acquireBackground(input)
		if !admitted {
			enricher.metrics.NotEnriched(input.PartnerID, ReasonBackgroundLimit)
			return Result{Outcome: OutcomeBackgroundLimit}, nil
		}
		if err := enricher.cache.PutInProgress(ctx, keys, input.Timeout); err != nil {
			enricher.logger.Warn(fmt.Sprintf("identity enrichment cache in-progress write failed: %v", err))
		}
		return enricher.execute(ctx, input, keys, release)
	}
}

func (enricher *enricher) validateRequest(input Request) error {
	if input.Timeout <= 0 {
		return errTimeoutNotPositive
	}
	if input.WaitTimeout != nil && *input.WaitTimeout < 0 {
		return errNegativeWaitTimeout
	}

	_, mode := normalizeWaitTimeout(input.Timeout, input.WaitTimeout)
	if mode == WaitModeSync {
		return nil
	}
	if enricher.cache == nil || !input.CacheEnabled {
		return errNonSyncCacheRequired
	}
	if cap(enricher.limiter) == 0 {
		return errNonSyncCapacityRequired
	}
	return nil
}

func (enricher *enricher) executeWithAdmission(ctx context.Context, input Request, keys []CacheKey) (Result, error) {
	release, admitted := enricher.acquireBackground(input)
	if !admitted {
		enricher.metrics.NotEnriched(input.PartnerID, ReasonBackgroundLimit)
		return Result{Outcome: OutcomeBackgroundLimit}, nil
	}
	return enricher.execute(ctx, input, keys, release)
}

func (enricher *enricher) acquireBackground(input Request) (func(), bool) {
	_, mode := normalizeWaitTimeout(input.Timeout, input.WaitTimeout)
	if mode == WaitModeSync {
		return nil, true
	}
	if !enricher.limiter.TryAcquire() {
		return nil, false
	}
	return func() {
		enricher.limiter.Release()
	}, true
}

func (enricher *enricher) execute(ctx context.Context, input Request, keys []CacheKey, release func()) (Result, error) {
	prepared := prepareResolution(input, keys)
	wait, mode := normalizeWaitTimeout(input.Timeout, input.WaitTimeout)
	if mode != WaitModeSync {
		ctx = context.WithoutCancel(ctx)
	}
	completion := enricher.startPrepared(ctx, prepared, release)
	if mode == WaitModeSync {
		completed := <-completion
		return completed.result, completed.err
	}
	if wait == 0 {
		enricher.metrics.NotEnriched(input.PartnerID, ReasonWaitExpired)
		return Result{Outcome: OutcomeWaitExpired}, nil
	}

	timer := time.NewTimer(wait)
	defer stopAndDrainTimer(timer)
	select {
	case completed := <-completion:
		return completed.result, completed.err
	case <-timer.C:
		// Prefer a result that completed at the wait boundary over reporting it
		// as late merely because select chose the timer case.
		select {
		case completed := <-completion:
			return completed.result, completed.err
		default:
			enricher.metrics.NotEnriched(input.PartnerID, ReasonWaitExpired)
			return Result{Outcome: OutcomeWaitExpired}, nil
		}
	}
}

func (enricher *enricher) startPrepared(
	ctx context.Context,
	prepared preparedResolution,
	release func(),
) <-chan resolutionCompletion {
	completed := make(chan resolutionCompletion, 1)
	go func() {
		if release != nil {
			defer release()
		}
		result, err := enricher.executePrepared(ctx, prepared)
		completed <- resolutionCompletion{result: result, err: err}
	}()
	return completed
}

func (enricher *enricher) executePrepared(ctx context.Context, prepared preparedResolution) (Result, error) {
	requestContext, cancel := context.WithTimeout(ctx, prepared.timeout)
	defer cancel()

	started := time.Now()
	response, err := enricher.s2s.Resolve(requestContext, prepared.requestURL, prepared.consent)
	duration := time.Since(started)

	enricher.metrics.APIRequestDuration(prepared.partnerID, duration)
	if err != nil {
		kind, status := classifyS2SError(err)
		enricher.logger.Warn(fmt.Sprintf(
			"identity enrichment S2S request failed: kind=%s status=%d: %v",
			kind, status, err,
		))
		enricher.metrics.APIError(prepared.partnerID, kind, status)
		return Result{}, err
	}

	enricher.metrics.APISuccess(prepared.partnerID)
	if response.EmptyBody {
		enricher.metrics.NotEnriched(prepared.partnerID, ReasonUnresolved)
		return Result{Outcome: OutcomeUnresolved}, nil
	}

	result := Result{
		EIDs:             response.EIDs(),
		CacheTTL:         response.TTL(),
		ABTestUUID:       response.ABTestUUID,
		TerminationCause: response.TC,
	}
	if len(result.EIDs) == 0 {
		result.Outcome = OutcomeNoIDs
		enricher.metrics.NotEnriched(prepared.partnerID, ReasonNoIDs)
		writeContext, cancelWrite := cacheWriteContext(ctx, prepared.timeout)
		defer cancelWrite()
		enricher.putNegative(writeContext, prepared.cacheKeys, result)
		return result, nil
	}

	result.Outcome = OutcomeEnriched
	enricher.metrics.Enriched(prepared.partnerID)
	if len(prepared.cacheKeys) > 0 {
		writeContext, cancelWrite := cacheWriteContext(ctx, prepared.timeout)
		defer cancelWrite()
		if err := enricher.cache.PutResolved(writeContext, prepared.cacheKeys, result); err != nil {
			enricher.logger.Warn(fmt.Sprintf("identity enrichment cache resolved write failed: %v", err))
		}
	}
	return result, nil
}

func (enricher *enricher) putNegative(ctx context.Context, keys []CacheKey, result Result) {
	if len(keys) == 0 {
		return
	}
	metadata := ResultMetadata{
		CacheTTL:         result.CacheTTL,
		ABTestUUID:       result.ABTestUUID,
		TerminationCause: result.TerminationCause,
	}
	if err := enricher.cache.PutNegative(ctx, keys, metadata); err != nil {
		enricher.logger.Warn(fmt.Sprintf("identity enrichment cache negative write failed: %v", err))
	}
}

func stopAndDrainTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func prepareResolution(input Request, keys []CacheKey) preparedResolution {
	requestURL, consent := buildS2SRequest(input)
	return preparedResolution{
		partnerID:  input.PartnerID,
		requestURL: requestURL,
		consent:    consent,
		timeout:    input.Timeout,
		cacheKeys:  append([]CacheKey(nil), keys...),
	}
}

// cacheWriteContext gives the cache write a budget of its own. The call that
// just finished may have spent the whole request timeout, and filling the cache
// is the reason it was allowed to outlive the auction that started it.
func cacheWriteContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, timeout)
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
