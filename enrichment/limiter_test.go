package enrichment

import "testing"

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
