package enrichment

// s2sLimiter bounds concurrent background S2S work. Slots are occupied which
// means sending takes one, receiving frees one. So len is work in
// flight, cap is the ceiling, and a full channel means no capacity left.
type s2sLimiter chan struct{}

func (limiter s2sLimiter) TryAcquire() bool {
	select {
	case limiter <- struct{}{}:
		return true
	default:
		return false
	}
}

func (limiter s2sLimiter) Release() {
	<-limiter
}

// Drain permanently acquires every permit. It blocks until all current holders
// release theirs, after which no further acquisition can succeed.
func (limiter s2sLimiter) Drain() {
	for range cap(limiter) {
		limiter <- struct{}{}
	}
}

func newS2SLimiter(maximum int) s2sLimiter {
	if maximum <= 0 {
		return nil
	}
	return make(s2sLimiter, maximum)
}
