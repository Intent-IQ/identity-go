package cache

import "time"

const (
	OperationGet = "get"
	OperationPut = "put"

	ResultHit    = "hit"
	ResultMiss   = "miss"
	ResultStored = "stored"
	ResultError  = "error"
)

type Metrics interface {
	L2Request(operation, result string)
	L2GetLatency(time.Duration)
	L2PutLatency(time.Duration)
}

type NoopMetrics struct{}

func (NoopMetrics) L2Request(string, string)   {}
func (NoopMetrics) L2GetLatency(time.Duration) {}
func (NoopMetrics) L2PutLatency(time.Duration) {}
