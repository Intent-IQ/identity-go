package enrichment

type backgroundLimiter interface {
	TryAcquire() bool
	Release()
}

type unlimitedBackgroundLimiter struct{}

func (unlimitedBackgroundLimiter) TryAcquire() bool { return true }
func (unlimitedBackgroundLimiter) Release()         {}

type boundedBackgroundLimiter chan struct{}

func (limiter boundedBackgroundLimiter) TryAcquire() bool {
	select {
	case limiter <- struct{}{}:
		return true
	default:
		return false
	}
}

func (limiter boundedBackgroundLimiter) Release() {
	<-limiter
}

func newBackgroundLimiter(maximum int) backgroundLimiter {
	if maximum <= 0 {
		return unlimitedBackgroundLimiter{}
	}
	return make(boundedBackgroundLimiter, maximum)
}
