package reporting

// reportLimiter bounds concurrent background reporting work. Slots are
// occupied which means sending takes one, receiving frees one. So len is work
// in flight, cap is the ceiling, and a full channel means no capacity left.
type reportLimiter chan struct{}

func (limiter reportLimiter) TryAcquire() bool {
	select {
	case limiter <- struct{}{}:
		return true
	default:
		return false
	}
}

func (limiter reportLimiter) Release() {
	<-limiter
}

// Drain permanently acquires every permit. It blocks until all current holders
// release theirs, after which no further acquisition can succeed.
func (limiter reportLimiter) Drain() {
	for range cap(limiter) {
		limiter <- struct{}{}
	}
}

func newReportLimiter(maximum int) reportLimiter {
	return make(reportLimiter, maximum)
}
