package enrichment

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Intent-IQ/identity-go/iiqapi/s2s"
	"github.com/prebid/openrtb/v20/openrtb2"
)

const asyncBenchmarkBatchSize = 1000

type benchmarkS2S struct {
}

func (api *benchmarkS2S) Resolve(context.Context, string, string) (s2s.Response, error) {
	return s2s.Response{ResolvedEIDs: []openrtb2.EID{{
		Source: "intentiq.com",
		UIDs:   []openrtb2.UID{{ID: "resolved"}},
	}}}, nil
}

type benchmarkCache struct {
	completed chan struct{}
}

func (benchmarkCache) Get(context.Context, []CacheKey) (CacheResult, error) {
	return CacheResult{State: CacheMiss}, nil
}

func (cache benchmarkCache) PutResolved(context.Context, []CacheKey, Result) error {
	cache.completed <- struct{}{}
	return nil
}
func (benchmarkCache) PutNegative(context.Context, []CacheKey, ResultMetadata) error {
	return nil
}
func (benchmarkCache) PutInProgress(context.Context, []CacheKey, time.Duration) error { return nil }

func BenchmarkEnrichAsync1000(b *testing.B) {
	api := &benchmarkS2S{}
	cache := benchmarkCache{completed: make(chan struct{}, asyncBenchmarkBatchSize)}
	created, err := New(Dependencies{
		S2S:                   api,
		Cache:                 cache,
		MaxBackgroundS2SCalls: asyncBenchmarkBatchSize,
	}, 1)
	if err != nil {
		b.Fatal(err)
	}

	wait := time.Duration(0)
	requests := make([]Request, asyncBenchmarkBatchSize)
	for i := range requests {
		requests[i] = Request{
			PartnerID:    "benchmark",
			Endpoint:     "https://example.test/resolve",
			Timeout:      time.Second,
			WaitTimeout:  &wait,
			CacheEnabled: true,
			Auction: &openrtb2.BidRequest{User: &openrtb2.User{EIDs: []openrtb2.EID{{
				Source: "pubcid.org",
				UIDs:   []openrtb2.UID{{ID: fmt.Sprintf("user-%d", i)}},
			}}}},
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		for _, request := range requests {
			result, err := created.Enrich(context.Background(), request)
			if err != nil || result.Outcome != OutcomeWaitExpired {
				b.Fatalf("Enrich() = (%#v, %v)", result, err)
			}
		}
		for range asyncBenchmarkBatchSize {
			<-cache.completed
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*asyncBenchmarkBatchSize), "ns/item")
}
