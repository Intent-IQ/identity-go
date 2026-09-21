package enrichment

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

func newS2SLimiter(maximum int) s2sLimiter {
	if maximum <= 0 {
		return nil
	}
	return make(s2sLimiter, maximum)
}
