package reporting

type Metrics interface {
	ImpressionReported(partnerID string)
	ImpressionError(partnerID string)
}

type NoopMetrics struct{}

func (NoopMetrics) ImpressionReported(string) {}
func (NoopMetrics) ImpressionError(string)    {}

var _ Metrics = NoopMetrics{}
