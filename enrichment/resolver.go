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
	errNonSyncCacheKeysRequired   = errors.New("cache keys are required for async or hybrid enrichment")
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

type executionPlan struct {
	mode WaitMode
	wait time.Duration
}

// resolutionJob contains everything execution needs after the host request
// is no longer safe to retain. In particular, it contains no auction pointer.
type resolutionJob struct {
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

func New(deps Dependencies, maxCacheKeys int) (Enricher, error) {
	if deps.S2S == nil {
		return nil, errS2SRequired
	}
	if deps.MaxBackgroundS2SCalls < 0 {
		return nil, errNegativeBackgroundS2SLimit
	}
	if deps.Metrics == nil {
		deps.Metrics = NoopMetrics{}
	}
	if deps.Logger == nil {
		deps.Logger = logging.NoopLogger{}
	}
	return &enricher{
		s2s:          deps.S2S,
		cache:        deps.Cache,
		metrics:      deps.Metrics,
		logger:       deps.Logger,
		maxCacheKeys: maxCacheKeys,
		limiter:      newS2SLimiter(deps.MaxBackgroundS2SCalls),
	}, nil
}

func (e *enricher) Enrich(ctx context.Context, input Request) (Result, error) {
	e.metrics.Request(input.PartnerID)

	if !notBlank(input.Endpoint) {
		e.metrics.NotEnriched(input.PartnerID, ReasonNoEndpoint)
		return Result{Outcome: OutcomeNoEndpoint}, nil
	}
	if input.Auction == nil {
		return Result{}, nil
	}
	plan, err := e.planRequest(input)
	if err != nil {
		return Result{}, err
	}

	if e.cache == nil || !input.CacheEnabled {
		return e.schedule(ctx, plan, newResolutionJob(input, nil), false)
	}

	keys := extractCacheKeys(input.Auction, e.maxCacheKeys)
	if len(keys) == 0 {
		if plan.mode != WaitModeSync {
			return Result{}, errNonSyncCacheKeysRequired
		}
		return e.schedule(ctx, plan, newResolutionJob(input, nil), false)
	}

	cached, err := e.cache.Get(ctx, keys)
	if err != nil {
		e.logger.Warn(fmt.Sprintf("identity enrichment cache read failed: %v", err))
		cached = CacheResult{State: CacheMiss, Layer: CacheLayerNone}
	}

	switch cached.State {
	case CacheHit:
		e.metrics.CacheLookup(input.PartnerID, CacheLookupHit, cached.Layer)
		result := cached.Result
		result.Outcome = OutcomeEnriched
		result.CacheLayer = cached.Layer
		e.metrics.Enriched(input.PartnerID)
		return result, nil
	case CacheNegative:
		e.metrics.CacheLookup(input.PartnerID, CacheLookupMiss, cached.Layer)
		result := cached.Result
		result.EIDs = nil
		result.Outcome = OutcomeCachedNoIDs
		result.CacheLayer = cached.Layer
		e.metrics.NotEnriched(input.PartnerID, ReasonNoIDsCached)
		return result, nil
	case CacheInProgress:
		e.metrics.CacheLookup(input.PartnerID, CacheLookupMiss, cached.Layer)
		e.metrics.NotEnriched(input.PartnerID, ReasonInProgress)
		return Result{Outcome: OutcomeInProgress, CacheLayer: cached.Layer}, nil
	default:
		e.metrics.CacheLookup(input.PartnerID, CacheLookupMiss, cached.Layer)
		return e.schedule(ctx, plan, newResolutionJob(input, keys), true)
	}
}

func (e *enricher) planRequest(input Request) (executionPlan, error) {
	if input.Timeout <= 0 {
		return executionPlan{}, errTimeoutNotPositive
	}
	if input.WaitTimeout != nil && *input.WaitTimeout < 0 {
		return executionPlan{}, errNegativeWaitTimeout
	}

	wait, mode := normalizeWaitTimeout(input.Timeout, input.WaitTimeout)
	plan := executionPlan{mode: mode, wait: wait}
	if mode == WaitModeSync {
		return plan, nil
	}
	if e.cache == nil || !input.CacheEnabled {
		return executionPlan{}, errNonSyncCacheRequired
	}
	if cap(e.limiter) == 0 {
		return executionPlan{}, errNonSyncCapacityRequired
	}
	return plan, nil
}

func (e *enricher) schedule(
	ctx context.Context,
	plan executionPlan,
	job resolutionJob,
	markInProgress bool,
) (Result, error) {
	if plan.mode == WaitModeSync {
		e.markInProgress(ctx, job, markInProgress)
		return e.run(ctx, job)
	}

	if !e.limiter.TryAcquire() {
		e.metrics.NotEnriched(job.partnerID, ReasonBackgroundLimit)
		return Result{Outcome: OutcomeBackgroundLimit}, nil
	}

	e.markInProgress(ctx, job, markInProgress)
	completion := e.runBackground(context.WithoutCancel(ctx), job)
	if plan.mode == WaitModeAsync {
		e.metrics.NotEnriched(job.partnerID, ReasonWaitExpired)
		return Result{Outcome: OutcomeWaitExpired}, nil
	}
	return e.waitForResult(completion, plan.wait, job.partnerID)
}

func (e *enricher) markInProgress(ctx context.Context, job resolutionJob, enabled bool) {
	if !enabled {
		return
	}
	if err := e.cache.PutInProgress(ctx, job.cacheKeys, job.timeout); err != nil {
		e.logger.Warn(fmt.Sprintf("identity enrichment cache in-progress write failed: %v", err))
	}
}

func (e *enricher) waitForResult(
	completion <-chan resolutionCompletion,
	wait time.Duration,
	partnerID string,
) (Result, error) {
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
			e.metrics.NotEnriched(partnerID, ReasonWaitExpired)
			return Result{Outcome: OutcomeWaitExpired}, nil
		}
	}
}

func (e *enricher) runBackground(ctx context.Context, job resolutionJob) <-chan resolutionCompletion {
	completed := make(chan resolutionCompletion, 1)
	go func() {
		defer e.limiter.Release()
		result, err := e.run(ctx, job)
		completed <- resolutionCompletion{result: result, err: err}
	}()
	return completed
}

func (e *enricher) run(ctx context.Context, job resolutionJob) (Result, error) {
	result, err := e.resolve(ctx, job)
	if err != nil {
		return Result{}, err
	}
	if result.Outcome != OutcomeUnresolved {
		e.storeResult(ctx, job, result)
	}
	return result, nil
}

func (e *enricher) resolve(ctx context.Context, job resolutionJob) (Result, error) {
	requestContext, cancel := context.WithTimeout(ctx, job.timeout)
	defer cancel()

	started := time.Now()
	response, err := e.s2s.Resolve(requestContext, job.requestURL, job.consent)
	duration := time.Since(started)

	e.metrics.APIRequestDuration(job.partnerID, duration)
	if err != nil {
		kind, status := classifyS2SError(err)
		e.logger.Warn(fmt.Sprintf(
			"identity enrichment S2S request failed: kind=%s status=%d: %v",
			kind, status, err,
		))
		e.metrics.APIError(job.partnerID, kind, status)
		return Result{}, err
	}

	e.metrics.APISuccess(job.partnerID)
	if response.EmptyBody {
		e.metrics.NotEnriched(job.partnerID, ReasonUnresolved)
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
		e.metrics.NotEnriched(job.partnerID, ReasonNoIDs)
		return result, nil
	}

	result.Outcome = OutcomeEnriched
	e.metrics.Enriched(job.partnerID)
	return result, nil
}

func (e *enricher) storeResult(ctx context.Context, job resolutionJob, result Result) {
	if len(job.cacheKeys) == 0 {
		return
	}

	writeCtx, cancel := context.WithTimeout(ctx, job.timeout)
	defer cancel()
	if result.Outcome == OutcomeNoIDs {
		e.putNegative(writeCtx, job.cacheKeys, result)
		return
	}
	if err := e.cache.PutResolved(writeCtx, job.cacheKeys, result); err != nil {
		e.logger.Warn(fmt.Sprintf("identity enrichment cache resolved write failed: %v", err))
	}
}

func (e *enricher) putNegative(ctx context.Context, keys []CacheKey, result Result) {
	if len(keys) == 0 {
		return
	}
	metadata := ResultMetadata{
		CacheTTL:         result.CacheTTL,
		ABTestUUID:       result.ABTestUUID,
		TerminationCause: result.TerminationCause,
	}
	if err := e.cache.PutNegative(ctx, keys, metadata); err != nil {
		e.logger.Warn(fmt.Sprintf("identity enrichment cache negative write failed: %v", err))
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

func newResolutionJob(input Request, keys []CacheKey) resolutionJob {
	requestURL, consent := buildS2SRequest(input)
	return resolutionJob{
		partnerID:  input.PartnerID,
		requestURL: requestURL,
		consent:    consent,
		timeout:    input.Timeout,
		cacheKeys:  append([]CacheKey(nil), keys...),
	}
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
