package reporting

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

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
