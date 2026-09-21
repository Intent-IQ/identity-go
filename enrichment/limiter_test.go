package enrichment

import (
	"sync"
	"testing"
)

type recordingBackgroundLimiter struct {
	mu       sync.Mutex
	allow    bool
	acquires int
	releases int
	released chan struct{}
}

func (limiter *recordingBackgroundLimiter) TryAcquire() bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.acquires++
	return limiter.allow
}

func (limiter *recordingBackgroundLimiter) Release() {
	limiter.mu.Lock()
	limiter.releases++
	limiter.mu.Unlock()
	if limiter.released != nil {
		limiter.released <- struct{}{}
	}
}

func (limiter *recordingBackgroundLimiter) counts() (int, int) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	return limiter.acquires, limiter.releases
}

func TestBackgroundLimiter(t *testing.T) {
	t.Run("bounded", func(t *testing.T) {
		limiter := newBackgroundLimiter(1)
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
	})

	for _, maximum := range []int{0, -1} {
		t.Run("unlimited", func(t *testing.T) {
			limiter := newBackgroundLimiter(maximum)
			for range 100 {
				if !limiter.TryAcquire() {
					t.Fatalf("unlimited limiter rejected acquisition for maximum %d", maximum)
				}
			}
			for range 100 {
				limiter.Release()
			}
		})
	}
}
