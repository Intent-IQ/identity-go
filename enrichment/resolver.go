package enrichment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Intent-IQ/identity-go/iiqapi"
	"github.com/Intent-IQ/identity-go/iiqapi/s2s"
	"github.com/Intent-IQ/identity-go/logging"
)

var (
	ErrShuttingDown = errors.New("identity enricher is shutting down")

	errS2SRequired                = errors.New("S2S API is required")
	errNegativeBackgroundS2SLimit = errors.New("max background S2S calls must not be negative")
	errTimeoutNotPositive         = errors.New("timeout must be positive")
	errNegativeWaitTimeout        = errors.New("wait timeout must not be negative")
	errNonSyncCacheRequired       = errors.New("cache must be enabled for async or hybrid enrichment")
	errNonSyncCacheKeysRequired   = errors.New("cache keys are required for async or hybrid enrichment")
	errNonSyncCapacityRequired    = errors.New("background S2S capacity must be positive for async or hybrid enrichment")
)

type enricher struct {
	logger logging.Logger

	s2s     s2s.API
	limiter s2sLimiter

	cache        Cache
	maxCacheKeys int

	metrics Metrics

	closing   atomic.Bool
	drainOnce sync.Once
	drained   chan struct{}
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

func (job resolutionJob) hasCacheKeys() bool {
	return len(job.cacheKeys) > 0
}

type resolutionCompletion struct {
	result Result
	err    error
}

type backgroundTask struct {
	resolutionCtx context.Context
	cacheCtx      context.Context
	cancel        context.CancelFunc
	job           resolutionJob
	completed     chan<- resolutionCompletion
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
	deps.Metrics.BackgroundCapacity(deps.MaxBackgroundS2SCalls)
	return &enricher{
		s2s:          deps.S2S,
		cache:        deps.Cache,
		metrics:      deps.Metrics,
		logger:       deps.Logger,
		maxCacheKeys: maxCacheKeys,
		limiter:      newS2SLimiter(deps.MaxBackgroundS2SCalls),
		drained:      make(chan struct{}),
	}, nil
}

func (e *enricher) Enrich(ctx context.Context, req Request) (Result, error) {
	if e.closing.Load() {
		return Result{}, ErrShuttingDown
	}

	e.metrics.Request(req.PartnerID)

	if !notBlank(req.Endpoint) {
		e.metrics.NotEnriched(req.PartnerID, ReasonNoEndpoint)
		return Result{Outcome: OutcomeNoEndpoint}, nil
	}
	if req.Auction == nil {
		return Result{}, nil
	}

	plan, err := e.executionPlanFor(req)
	if err != nil {
		return Result{}, err
	}
	keys, err := e.cacheKeysFor(req, plan)
	if err != nil {
		return Result{}, err
	}
	if result, found := e.lookupCache(ctx, req.PartnerID, keys); found {
		return result, nil
	}
	return e.schedule(ctx, plan, newResolutionJob(req, keys))
}

func (e *enricher) cacheKeysFor(input Request, plan executionPlan) ([]CacheKey, error) {
	if e.cache == nil || !input.CacheEnabled {
		return nil, nil
	}
	keys := extractCacheKeys(input.Auction, e.maxCacheKeys)
	if len(keys) == 0 && plan.mode != WaitModeSync {
		return nil, errNonSyncCacheKeysRequired
	}
	return keys, nil
}

func (e *enricher) lookupCache(ctx context.Context, partnerID string, keys []CacheKey) (Result, bool) {
	if len(keys) == 0 {
		return Result{}, false
	}
	cached, err := e.cache.Get(ctx, keys)
	if err != nil {
		e.logger.Warn(fmt.Sprintf("identity enrichment cache read failed: %v", err))
		cached = CacheResult{State: CacheMiss, Layer: CacheLayerNone}
	}

	switch cached.State {
	case CacheHit:
		e.metrics.CacheLookup(partnerID, CacheLookupHit, cached.Layer)
		result := cached.Result
		result.Outcome = OutcomeEnriched
		result.CacheLayer = cached.Layer
		e.metrics.Enriched(partnerID)
		return result, true
	case CacheNegative:
		e.metrics.CacheLookup(partnerID, CacheLookupMiss, cached.Layer)
		result := cached.Result
		result.EIDs = nil
		result.Outcome = OutcomeCachedNoIDs
		result.CacheLayer = cached.Layer
		e.metrics.NotEnriched(partnerID, ReasonNoIDsCached)
		return result, true
	case CacheInProgress:
		e.metrics.CacheLookup(partnerID, CacheLookupMiss, cached.Layer)
		e.metrics.NotEnriched(partnerID, ReasonInProgress)
		return Result{Outcome: OutcomeInProgress, CacheLayer: cached.Layer}, true
	default:
		e.metrics.CacheLookup(partnerID, CacheLookupMiss, cached.Layer)
		return Result{}, false
	}
}

func (e *enricher) executionPlanFor(req Request) (executionPlan, error) {
	if req.Timeout <= 0 {
		return executionPlan{}, errTimeoutNotPositive
	}
	if req.WaitTimeout != nil && *req.WaitTimeout < 0 {
		return executionPlan{}, errNegativeWaitTimeout
	}

	wait, mode := normalizeWaitTimeout(req.Timeout, req.WaitTimeout)
	plan := executionPlan{mode: mode, wait: wait}
	if mode == WaitModeSync {
		return plan, nil
	}
	if e.cache == nil || !req.CacheEnabled {
		return executionPlan{}, errNonSyncCacheRequired
	}
	if cap(e.limiter) == 0 {
		return executionPlan{}, errNonSyncCapacityRequired
	}
	return plan, nil
}

func (e *enricher) schedule(ctx context.Context, plan executionPlan, job resolutionJob) (Result, error) {
	if plan.mode == WaitModeSync {
		e.markInProgress(ctx, job)
		resolutionCtx, cancel := context.WithTimeout(ctx, job.timeout)
		defer cancel()
		return e.runBounded(resolutionCtx, ctx, job)
	}
	if !e.limiter.TryAcquire() {
		if e.closing.Load() {
			return Result{}, ErrShuttingDown
		}
		e.metrics.NotEnriched(job.partnerID, ReasonBackgroundLimit)
		return Result{Outcome: OutcomeBackgroundLimit}, nil
	}
	// Shutdown may have started between the first closing check and permit
	// acquisition. Return the permit instead of admitting new background work.
	if e.closing.Load() {
		e.limiter.Release()
		return Result{}, ErrShuttingDown
	}
	e.metrics.BackgroundStarted()

	e.markInProgress(ctx, job)

	cacheCtx := context.WithoutCancel(ctx)
	resolutionCtx, cancel := context.WithTimeout(cacheCtx, job.timeout)

	task := backgroundTask{
		resolutionCtx: resolutionCtx,
		cacheCtx:      cacheCtx,
		cancel:        cancel,
		job:           job,
	}

	if plan.mode == WaitModeAsync {
		e.startBackground(task)
		return e.waitExpired(job.partnerID)
	}

	completion := make(chan resolutionCompletion, 1)
	task.completed = completion

	e.startBackground(task)
	return e.waitForResult(completion, plan.wait, job.partnerID)
}

func (e *enricher) markInProgress(ctx context.Context, job resolutionJob) {
	if !job.hasCacheKeys() {
		return
	}
	if err := e.cache.PutInProgress(ctx, job.cacheKeys, job.timeout); err != nil {
		e.logger.Warn(fmt.Sprintf("identity enrichment cache in-progress write failed: %v", err))
	}
}

func (e *enricher) startBackground(task backgroundTask) {
	go func() {
		defer func() {
			e.metrics.BackgroundFinished()
			e.limiter.Release()
		}()

		result, err := e.resolve(task.resolutionCtx, task.job)
		task.cancel()

		// The current auction depends only on S2S resolution. Cache persistence
		// may continue after the caller's wait budget expires.
		if task.completed != nil {
			task.completed <- resolutionCompletion{result: result, err: err}
		}
		if err == nil {
			e.storeResult(task.cacheCtx, task.job, result)
		}
	}()
}

func (e *enricher) Shutdown(ctx context.Context) error {
	e.closing.Store(true)
	e.drainOnce.Do(func() {
		go func() {
			e.limiter.Drain()
			close(e.drained)
		}()
	})

	select {
	case <-e.drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *enricher) waitForResult(completion <-chan resolutionCompletion, wait time.Duration, partnerID string) (Result, error) {
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
			return e.waitExpired(partnerID)
		}
	}
}

func (e *enricher) waitExpired(partnerID string) (Result, error) {
	e.metrics.NotEnriched(partnerID, ReasonWaitExpired)
	return Result{Outcome: OutcomeWaitExpired}, nil
}

func (e *enricher) run(ctx context.Context, job resolutionJob) (Result, error) {
	resolutionCtx, cancel := context.WithTimeout(ctx, job.timeout)
	defer cancel()
	return e.runBounded(resolutionCtx, ctx, job)
}

func (e *enricher) runBounded(resolutionCtx, cacheCtx context.Context, job resolutionJob) (Result, error) {
	result, err := e.resolve(resolutionCtx, job)
	if err != nil {
		return Result{}, err
	}
	e.storeResult(cacheCtx, job, result)
	return result, nil
}

func (e *enricher) resolve(ctx context.Context, job resolutionJob) (Result, error) {
	started := time.Now()
	response, err := e.s2s.Resolve(ctx, job.requestURL, job.consent)
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
	if !job.hasCacheKeys() {
		return
	}

	// Give persistence a fresh budget: resolution may have consumed the full
	// S2S timeout, while warming the cache is the purpose of background work.
	writeCtx, cancel := context.WithTimeout(ctx, job.timeout)
	defer cancel()
	switch result.Outcome {
	case OutcomeEnriched:
		if err := e.cache.PutResolved(writeCtx, job.cacheKeys, result); err != nil {
			e.logger.Warn(fmt.Sprintf("identity enrichment cache resolved write failed: %v", err))
		}
	case OutcomeNoIDs:
		metadata := ResultMetadata{
			CacheTTL:         result.CacheTTL,
			ABTestUUID:       result.ABTestUUID,
			TerminationCause: result.TerminationCause,
		}
		if err := e.cache.PutNegative(writeCtx, job.cacheKeys, metadata); err != nil {
			e.logger.Warn(fmt.Sprintf("identity enrichment cache negative write failed: %v", err))
		}
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
