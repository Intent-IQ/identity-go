package reporting

import "time"

type Metrics interface {
	ImpressionRejected(partnerID string, reason ReportResult)
	ImpressionAPIRequestDuration(partnerID string, duration time.Duration)
	ImpressionReported(partnerID string)
	ImpressionError(partnerID string)
	ReportingCapacity(capacity int)
	ReportingStarted()
	ReportingFinished()
}

type NoopMetrics struct{}

func (NoopMetrics) ImpressionRejected(string, ReportResult)            {}
func (NoopMetrics) ImpressionAPIRequestDuration(string, time.Duration) {}
func (NoopMetrics) ImpressionReported(string)                          {}
func (NoopMetrics) ImpressionError(string)                             {}
func (NoopMetrics) ReportingCapacity(int)                              {}
func (NoopMetrics) ReportingStarted()                                  {}
func (NoopMetrics) ReportingFinished()                                 {}

var _ Metrics = NoopMetrics{}
