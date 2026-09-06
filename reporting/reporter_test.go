package reporting

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Intent-IQ/identity-go/iiqapi"
	"github.com/prebid/openrtb/v20/openrtb2"
)

func TestNewReporterRequiresAPI(t *testing.T) {
	reporter, err := New(Dependencies{})
	if !errors.Is(err, errAPIRequired) {
		t.Fatalf("New() error = %v, want %v", err, errAPIRequired)
	}
	if reporter != nil {
		t.Fatalf("New() reporter = %#v, want nil", reporter)
	}
}

func TestNewReporterDefaultsNilDependencies(t *testing.T) {
	api := &reportingAPIStub{}
	reporter, err := New(Dependencies{API: api})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := reporter.Report(t.Context(), baseReportingRequest()); err != nil {
		t.Fatalf("Report() error = %v", err)
	}
}

func TestReporterEmptyEndpointIsNoop(t *testing.T) {
	recorder := &eventRecorder{}
	api := &reportingAPIStub{events: recorder}
	metrics := &reportingMetricsStub{events: recorder}
	logger := &reportingLoggerStub{events: recorder}
	reporter := mustNewReporter(t, Dependencies{API: api, Metrics: metrics, Logger: logger})

	request := baseReportingRequest()
	request.Endpoint = ""
	if err := reporter.Report(t.Context(), request); err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	if events := recorder.snapshot(); len(events) != 0 {
		t.Fatalf("events = %v, want none", events)
	}
}

func TestReporterWhitespaceEndpointIsNotEmpty(t *testing.T) {
	api := &reportingAPIStub{}
	reporter := mustNewReporter(t, Dependencies{API: api})
	request := baseReportingRequest()
	request.Endpoint = " "

	if err := reporter.Report(t.Context(), request); err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	if got := api.callCount(); got != 1 {
		t.Fatalf("API calls = %d, want 1", got)
	}
}

func TestReporterSuccessForwardsExactURLAndRecordsMetric(t *testing.T) {
	recorder := &eventRecorder{}
	api := &reportingAPIStub{events: recorder}
	metrics := &reportingMetricsStub{events: recorder}
	logger := &reportingLoggerStub{events: recorder}
	reporter := mustNewReporter(t, Dependencies{API: api, Metrics: metrics, Logger: logger})

	request := baseReportingRequest()
	if err := reporter.Report(t.Context(), request); err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	wantURL := "https://reports.example/path?at=45&rtype=1&source=pbsgo&dpi=partner&rdata=%7B%22bidderCode%22%3A%22bidder%22%2C%22partnerId%22%3A%22partner%22%2C%22cpm%22%3A1.5%2C%22currency%22%3A%22EUR%22%2C%22placementId%22%3A%22imp%22%2C%22biddingPlatformId%22%3A%224%22%7D"
	if got := api.lastURL(); got != wantURL {
		t.Fatalf("API URL = %q, want %q", got, wantURL)
	}
	assertEvents(t, recorder, "api", "reported:partner")
}

func TestReporterReturnsOriginalAPIErrorsAndRecordsFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "request", err: &iiqapi.Error{Kind: iiqapi.ErrorRequest, Err: errors.New("invalid URL")}},
		{name: "transport", err: &iiqapi.Error{Kind: iiqapi.ErrorTransport, Err: errors.New("connection failed")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := &eventRecorder{}
			api := &reportingAPIStub{err: test.err, events: recorder}
			metrics := &reportingMetricsStub{events: recorder}
			logger := &reportingLoggerStub{events: recorder}
			reporter := mustNewReporter(t, Dependencies{API: api, Metrics: metrics, Logger: logger})

			err := reporter.Report(t.Context(), baseReportingRequest())
			if err != test.err {
				t.Fatalf("Report() error = %v, want original error %v", err, test.err)
			}
			assertEvents(t, recorder,
				"api",
				"error:partner",
				fmt.Sprintf("warn:intentiq-identity: impression report failed (dpi=partner): %v", test.err),
			)
		})
	}
}

func TestReporterAppliesPositiveTimeoutAroundAPICall(t *testing.T) {
	api := &reportingAPIStub{call: func(ctx context.Context, _ string) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			return errors.New("API context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > time.Second {
			return fmt.Errorf("unexpected remaining timeout: %v", remaining)
		}
		return nil
	}}
	reporter := mustNewReporter(t, Dependencies{API: api})
	request := baseReportingRequest()
	request.Timeout = time.Second

	if err := reporter.Report(t.Context(), request); err != nil {
		t.Fatalf("Report() error = %v", err)
	}
}

func TestReporterZeroAndNegativeTimeoutExpireImmediately(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			api := &reportingAPIStub{call: func(ctx context.Context, _ string) error {
				return ctx.Err()
			}}
			reporter := mustNewReporter(t, Dependencies{API: api})
			request := baseReportingRequest()
			request.Timeout = timeout

			if err := reporter.Report(t.Context(), request); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Report() error = %v, want context.DeadlineExceeded", err)
			}
		})
	}
}

func TestReporterDoesNotCancelCallerContext(t *testing.T) {
	callerContext, cancel := context.WithCancel(t.Context())
	defer cancel()
	reporter := mustNewReporter(t, Dependencies{API: &reportingAPIStub{}})

	if err := reporter.Report(callerContext, baseReportingRequest()); err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	if err := callerContext.Err(); err != nil {
		t.Fatalf("caller context error = %v, want nil", err)
	}
}

func TestReporterConcurrentCalls(t *testing.T) {
	api := &reportingAPIStub{}
	metrics := &reportingMetricsStub{}
	reporter := mustNewReporter(t, Dependencies{API: api, Metrics: metrics})

	const reportCount = 32
	errorsFound := make(chan error, reportCount)
	var waitGroup sync.WaitGroup
	for range reportCount {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if err := reporter.Report(t.Context(), baseReportingRequest()); err != nil {
				errorsFound <- err
			}
		}()
	}
	waitGroup.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("Report() error = %v", err)
	}
	if got := api.callCount(); got != reportCount {
		t.Fatalf("API calls = %d, want %d", got, reportCount)
	}
	if got := metrics.reportedCount(); got != reportCount {
		t.Fatalf("reported metrics = %d, want %d", got, reportCount)
	}
}

func baseReportingRequest() Request {
	return Request{
		PartnerID:  "partner",
		Endpoint:   "https://reports.example/path",
		Timeout:    time.Second,
		Bid:        openrtb2.Bid{ImpID: "imp", Price: 1.5},
		BidderCode: "bidder",
		Currency:   "EUR",
	}
}

func mustNewReporter(t *testing.T, dependencies Dependencies) Reporter {
	t.Helper()
	reporter, err := New(dependencies)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return reporter
}

type reportingAPIStub struct {
	mutex  sync.Mutex
	urls   []string
	err    error
	call   func(context.Context, string) error
	events *eventRecorder
}

func (api *reportingAPIStub) ReportImpression(ctx context.Context, requestURL string) error {
	api.mutex.Lock()
	api.urls = append(api.urls, requestURL)
	api.mutex.Unlock()
	if api.events != nil {
		api.events.add("api")
	}
	if api.call != nil {
		return api.call(ctx, requestURL)
	}
	return api.err
}

func (api *reportingAPIStub) callCount() int {
	api.mutex.Lock()
	defer api.mutex.Unlock()
	return len(api.urls)
}

func (api *reportingAPIStub) lastURL() string {
	api.mutex.Lock()
	defer api.mutex.Unlock()
	return api.urls[len(api.urls)-1]
}

type reportingMetricsStub struct {
	mutex    sync.Mutex
	reported int
	errors   int
	events   *eventRecorder
}

func (metrics *reportingMetricsStub) ImpressionReported(partnerID string) {
	metrics.mutex.Lock()
	metrics.reported++
	metrics.mutex.Unlock()
	if metrics.events != nil {
		metrics.events.add("reported:" + partnerID)
	}
}

func (metrics *reportingMetricsStub) ImpressionError(partnerID string) {
	metrics.mutex.Lock()
	metrics.errors++
	metrics.mutex.Unlock()
	if metrics.events != nil {
		metrics.events.add("error:" + partnerID)
	}
}

func (metrics *reportingMetricsStub) reportedCount() int {
	metrics.mutex.Lock()
	defer metrics.mutex.Unlock()
	return metrics.reported
}

type reportingLoggerStub struct {
	events *eventRecorder
}

func (logger *reportingLoggerStub) Debug(message string) {
	if logger.events != nil {
		logger.events.add("debug:" + message)
	}
}

func (logger *reportingLoggerStub) Warn(message string) {
	if logger.events != nil {
		logger.events.add("warn:" + message)
	}
}

func (logger *reportingLoggerStub) Error(message string) {
	if logger.events != nil {
		logger.events.add("error-log:" + message)
	}
}

type eventRecorder struct {
	mutex  sync.Mutex
	events []string
}

func (recorder *eventRecorder) add(event string) {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	recorder.events = append(recorder.events, event)
}

func (recorder *eventRecorder) snapshot() []string {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	return append([]string(nil), recorder.events...)
}

func assertEvents(t *testing.T, recorder *eventRecorder, want ...string) {
	t.Helper()
	got := recorder.snapshot()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

var _ Reporter = (*reporter)(nil)
