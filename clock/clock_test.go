package clock

import (
	"testing"
	"time"
)

func TestRealClock(t *testing.T) {
	before := time.Now()
	got := (RealClock{}).Now()
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Fatalf("RealClock.Now() = %v, want a time between %v and %v", got, before, after)
	}
}
