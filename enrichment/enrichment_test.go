package enrichment

import (
	"testing"
	"time"
)

func TestStableTokens(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "outcome enriched", got: string(OutcomeEnriched), want: "enriched"},
		{name: "outcome no ids", got: string(OutcomeNoIDs), want: "no_ids"},
		{name: "outcome cached no ids", got: string(OutcomeCachedNoIDs), want: "no_ids_cached"},
		{name: "outcome unresolved", got: string(OutcomeUnresolved), want: "unresolved"},
		{name: "outcome in progress", got: string(OutcomeInProgress), want: "in_progress"},
		{name: "outcome no endpoint", got: string(OutcomeNoEndpoint), want: "no_endpoint"},
		{name: "outcome wait expired", got: string(OutcomeWaitExpired), want: "wait_expired"},
		{name: "outcome background limit", got: string(OutcomeBackgroundLimit), want: "background_limit"},
		{name: "wait mode sync", got: string(WaitModeSync), want: "sync"},
		{name: "wait mode async", got: string(WaitModeAsync), want: "async"},
		{name: "wait mode hybrid", got: string(WaitModeHybrid), want: "hybrid"},
		{name: "reason no ids", got: string(ReasonNoIDs), want: "no_ids"},
		{name: "reason cached no ids", got: string(ReasonNoIDsCached), want: "no_ids_cached"},
		{name: "reason unresolved", got: string(ReasonUnresolved), want: "unresolved"},
		{name: "reason in progress", got: string(ReasonInProgress), want: "in_progress"},
		{name: "reason no endpoint", got: string(ReasonNoEndpoint), want: "no_endpoint"},
		{name: "reason wait expired", got: string(ReasonWaitExpired), want: "wait_expired"},
		{name: "reason background limit", got: string(ReasonBackgroundLimit), want: "background_limit"},
		{name: "cache hit", got: string(CacheLookupHit), want: "hit"},
		{name: "cache miss", got: string(CacheLookupMiss), want: "miss"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("token = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestNormalizeWaitTimeout(t *testing.T) {
	timeout := time.Second
	duration := func(value time.Duration) *time.Duration { return &value }

	tests := []struct {
		name     string
		timeout  time.Duration
		wait     *time.Duration
		wantWait time.Duration
		wantMode WaitMode
	}{
		{name: "absent remains synchronous", timeout: timeout, wantWait: timeout, wantMode: WaitModeSync},
		{name: "explicit zero is asynchronous", timeout: timeout, wait: duration(0), wantWait: 0, wantMode: WaitModeAsync},
		{name: "negative clamps to asynchronous", timeout: timeout, wait: duration(-time.Millisecond), wantWait: 0, wantMode: WaitModeAsync},
		{name: "between zero and timeout is hybrid", timeout: timeout, wait: duration(250 * time.Millisecond), wantWait: 250 * time.Millisecond, wantMode: WaitModeHybrid},
		{name: "equal to timeout is synchronous", timeout: timeout, wait: duration(timeout), wantWait: timeout, wantMode: WaitModeSync},
		{name: "above timeout is synchronous", timeout: timeout, wait: duration(2 * timeout), wantWait: timeout, wantMode: WaitModeSync},
		{name: "zero call timeout preserves synchronous behavior", timeout: 0, wait: duration(0), wantWait: 0, wantMode: WaitModeSync},
		{name: "negative call timeout preserves synchronous behavior", timeout: -time.Millisecond, wait: duration(0), wantWait: -time.Millisecond, wantMode: WaitModeSync},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotWait, gotMode := normalizeWaitTimeout(test.timeout, test.wait)
			if gotWait != test.wantWait || gotMode != test.wantMode {
				t.Fatalf("normalizeWaitTimeout(%v, %v) = (%v, %q), want (%v, %q)",
					test.timeout, test.wait, gotWait, gotMode, test.wantWait, test.wantMode)
			}
		})
	}
}
