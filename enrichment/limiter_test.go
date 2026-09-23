package enrichment

import (
	"testing"
	"time"
)

func TestS2SLimiter(t *testing.T) {
	limiter := newS2SLimiter(1)
	if !limiter.TryAcquire() {
		t.Fatal("first acquisition was rejected")
	}
	if limiter.TryAcquire() {
		t.Fatal("acquisition above capacity succeeded")
	}
	limiter.Release()
	if !limiter.TryAcquire() {
		t.Fatal("permit was not released")
	}
	limiter.Release()
}

func TestS2SLimiterWithoutCapacityRejects(t *testing.T) {
	for _, maximum := range []int{0, -1} {
		if newS2SLimiter(maximum).TryAcquire() {
			t.Fatalf("acquisition succeeded with maximum %d", maximum)
		}
	}
}

func TestS2SLimiterDrainWaitsForCurrentHoldersAndClosesAdmission(t *testing.T) {
	limiter := newS2SLimiter(2)
	if !limiter.TryAcquire() {
		t.Fatal("initial acquisition was rejected")
	}

	drained := make(chan struct{})
	go func() {
		limiter.Drain()
		close(drained)
	}()

	deadline := time.Now().Add(time.Second)
	for len(limiter) < cap(limiter) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(limiter) != cap(limiter) {
		t.Fatal("Drain() did not acquire the available permit")
	}
	select {
	case <-drained:
		t.Fatal("Drain() returned while a permit was still held")
	default:
	}

	limiter.Release()
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("Drain() did not return after the permit was released")
	}
	if limiter.TryAcquire() {
		t.Fatal("acquisition succeeded after Drain()")
	}
}
