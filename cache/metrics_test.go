package cache

import (
	"testing"
	"time"
)

func TestNoopMetrics(t *testing.T) {
	metrics := NoopMetrics{}
	metrics.L2Request(OperationGet, ResultMiss)
	metrics.L2GetLatency(time.Millisecond)
	metrics.L2PutLatency(time.Millisecond)
}

func TestStableMetricTokens(t *testing.T) {
	tests := map[string]string{
		OperationGet: "get",
		OperationPut: "put",
		ResultHit:    "hit",
		ResultMiss:   "miss",
		ResultStored: "stored",
		ResultError:  "error",
	}
	for got, want := range tests {
		if got != want {
			t.Fatalf("token = %q, want %q", got, want)
		}
	}
}
