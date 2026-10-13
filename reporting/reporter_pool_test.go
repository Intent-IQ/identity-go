package reporting

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestReporterSnapshotsRequestBeforeReturning(t *testing.T) {
	urls := make(chan string, 1)
	api := &reportingAPIStub{call: func(_ context.Context, requestURL string) error {
		urls <- requestURL
		return nil
	}}
	value, err := New(Dependencies{API: api}, Config{MaxConcurrentCalls: 1})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ext := json.RawMessage(`{"origbidcpm":1.25}`)
	terminationCause := int64(7)
	request := baseReportingRequest()
	request.Bid.Ext = ext
	request.TerminationCause = &terminationCause

	if got := value.TryReport(request); got != ReportAccepted {
		t.Fatalf("report result = %q, want %q", got, ReportAccepted)
	}

	copy(ext, `{"origbidcpm":9.99}`)
	terminationCause = 99

	var requestURL string
	select {
	case requestURL = <-urls:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for report")
	}

	rdata := decodeRData(t, requestURL)
	assertJSONNumber(t, rdata, "originalCpm", "1.25")
	assertJSONNumber(t, rdata, "terminationCause", "7")

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := value.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestReporterRecoversWorkerPanicAndReleasesCapacity(t *testing.T) {
	var calls atomic.Int64
	api := &reportingAPIStub{call: func(context.Context, string) error {
		if calls.Add(1) == 1 {
			panic("reporting API panic")
		}
		return nil
	}}
	metrics := &reportingMetricsStub{}
	panicLogs := make(chan string, 1)
	value, err := New(
		Dependencies{API: api, Metrics: metrics, Logger: &reportingLoggerStub{errors: panicLogs}},
		Config{MaxConcurrentCalls: 1},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got := value.TryReport(baseReportingRequest()); got != ReportAccepted {
		t.Fatalf("first report result = %q, want %q", got, ReportAccepted)
	}

	select {
	case message := <-panicLogs:
		if !strings.Contains(message, "reporting API panic") || !strings.Contains(message, "goroutine") {
			t.Fatalf("panic log = %q, want panic and stack", message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for panic recovery")
	}

	if gotActive, gotCapacity := metrics.reportingState(); gotActive != 0 || gotCapacity != 1 {
		t.Fatalf("reporting state after panic = active %d capacity %d, want active 0 capacity 1", gotActive, gotCapacity)
	}
	if got := metrics.errorCount(); got != 1 {
		t.Fatalf("reporting errors after panic = %d, want 1", got)
	}
	if got := value.TryReport(baseReportingRequest()); got != ReportAccepted {
		t.Fatalf("second report result = %q, want %q", got, ReportAccepted)
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := value.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestReporterBoundsConcurrentCalls(t *testing.T) {
	events := &eventRecorder{}
	started := make(chan struct{}, 2)
	release := make(chan struct{})

	var active atomic.Int64
	var maximum atomic.Int64

	metrics := &reportingMetricsStub{}
	api := &reportingAPIStub{call: func(ctx context.Context, _ string) error {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			seen := maximum.Load()
			if current <= seen || maximum.CompareAndSwap(seen, current) {
				break
			}
		}
		started <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	value, err := New(
		Dependencies{API: api, Metrics: metrics, Logger: &reportingLoggerStub{events: events}},
		Config{MaxConcurrentCalls: 2},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got := value.TryReport(baseReportingRequest()); got != ReportAccepted {
		t.Fatalf("first report result = %q, want %q", got, ReportAccepted)
	}
	waitForSignal(t, started)

	if got := value.TryReport(baseReportingRequest()); got != ReportAccepted {
		t.Fatalf("second report result = %q, want %q", got, ReportAccepted)
	}
	waitForSignal(t, started)

	if got := value.TryReport(baseReportingRequest()); got != ReportAtCapacity {
		t.Fatalf("saturated report result = %q, want %q", got, ReportAtCapacity)
	}

	if got := metrics.rejectedCount(ReportAtCapacity); got != 1 {
		t.Fatalf("at-capacity rejections = %d, want 1", got)
	}
	if gotActive, gotCapacity := metrics.reportingState(); gotActive != 2 || gotCapacity != 2 {
		t.Fatalf("reporting state = active %d capacity %d, want active 2 capacity 2", gotActive, gotCapacity)
	}
	assertEvents(t, events, "warn:intentiq-identity: impression report rejected (dpi=partner): at_capacity")
	if got := maximum.Load(); got != 2 {
		t.Fatalf("maximum active reports = %d, want 2", got)
	}

	close(release)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := value.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if got := value.TryReport(baseReportingRequest()); got != ReportShuttingDown {
		t.Fatalf("post-shutdown report result = %q, want %q", got, ReportShuttingDown)
	}
	if got := metrics.rejectedCount(ReportShuttingDown); got != 1 {
		t.Fatalf("shutdown rejections = %d, want 1", got)
	}
	if gotActive, gotCapacity := metrics.reportingState(); gotActive != 0 || gotCapacity != 2 {
		t.Fatalf("reporting state after shutdown = active %d capacity %d, want active 0 capacity 2", gotActive, gotCapacity)
	}
	assertEvents(t, events,
		"warn:intentiq-identity: impression report rejected (dpi=partner): at_capacity",
		"warn:intentiq-identity: impression report rejected (dpi=partner): shutting_down",
	)
	if err := value.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown() error = %v", err)
	}
}

func TestReporterShutdownCancelsOnDeadline(t *testing.T) {
	started := make(chan struct{}, 1)
	api := &reportingAPIStub{call: func(ctx context.Context, _ string) error {
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}}
	value, err := New(Dependencies{API: api}, Config{MaxConcurrentCalls: 1})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got := value.TryReport(baseReportingRequest()); got != ReportAccepted {
		t.Fatalf("report result = %q, want %q", got, ReportAccepted)
	}
	waitForSignal(t, started)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := value.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown() error = %v, want context deadline", err)
	}

	reporter := value.(*reporter)
	select {
	case <-reporter.drained:
	case <-time.After(time.Second):
		t.Fatal("report did not stop after cancellation")
	}
}

func waitForSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for report")
	}
}
