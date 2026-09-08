package cache

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Intent-IQ/identity-go/enrichment"
	"github.com/prebid/openrtb/v20/openrtb2"
)

type mutableClock struct {
	mu  sync.RWMutex
	now time.Time
}

func (clock *mutableClock) Now() time.Time {
	clock.mu.RLock()
	defer clock.mu.RUnlock()
	return clock.now
}

func (clock *mutableClock) set(now time.Time) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = now
}

func (clock *mutableClock) advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration)
}

func TestLocalCacheGetSetAndMiss(t *testing.T) {
	clock := &mutableClock{now: time.UnixMilli(1_700_000_000_000)}
	codec := newEntryCodec(clock)
	local := newLocalCache(1024*1024, codec)
	want := codec.resolved(enrichment.Result{
		EIDs:       []openrtb2.EID{{Source: "intentiq.com"}},
		ABTestUUID: "ab-1",
	}, time.Minute)
	encoded, err := codec.encode(want)
	if err != nil {
		t.Fatalf("encode() error = %v", err)
	}
	if err := local.set("key", encoded, time.Minute); err != nil {
		t.Fatalf("set() error = %v", err)
	}

	got, found := local.get("key")
	if !found || !reflect.DeepEqual(got, want) {
		t.Fatalf("get() = (%#v, %t), want (%#v, true)", got, found, want)
	}
	if _, found := local.get("missing"); found {
		t.Fatal("get() found missing key")
	}
}

func TestLocalCacheValidatesAbsoluteExpiryAfterEveryRead(t *testing.T) {
	start := time.UnixMilli(1_700_000_000_000)
	clock := &mutableClock{now: start}
	codec := newEntryCodec(clock)
	local := newLocalCache(0, codec)
	value := codec.inProgress(time.Minute)
	encoded, err := codec.encode(value)
	if err != nil {
		t.Fatalf("encode() error = %v", err)
	}
	if err := local.set("key", encoded, time.Hour); err != nil {
		t.Fatalf("set() error = %v", err)
	}

	if _, found := local.get("key"); !found {
		t.Fatal("get() missed live entry")
	}
	clock.set(start.Add(time.Minute))
	if _, found := local.get("key"); found {
		t.Fatal("get() returned entry at exact absolute-expiry boundary")
	}
}

func TestLocalCacheTreatsMalformedEntryAsMiss(t *testing.T) {
	local := newLocalCache(0, newEntryCodec(&mutableClock{now: time.UnixMilli(1)}))
	if err := local.set("bad", []byte(`{bad`), time.Minute); err != nil {
		t.Fatalf("set() error = %v", err)
	}
	if _, found := local.get("bad"); found {
		t.Fatal("get() returned malformed entry")
	}
}

func TestLocalCacheCapacity(t *testing.T) {
	tests := []struct {
		name      string
		requested int
		want      int
	}{
		{"zero uses minimum", 0, minimumFreeCacheSize},
		{"below minimum uses minimum", minimumFreeCacheSize - 1, minimumFreeCacheSize},
		{"minimum retained", minimumFreeCacheSize, minimumFreeCacheSize},
		{"larger byte budget retained", 2 * minimumFreeCacheSize, 2 * minimumFreeCacheSize},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			local := newLocalCache(test.requested, newEntryCodec(nil))
			if local.capacity != test.want {
				t.Fatalf("capacity = %d, want %d", local.capacity, test.want)
			}
		})
	}
}

func TestLocalCacheRejectsEntryLargerThanByteBudget(t *testing.T) {
	local := newLocalCache(minimumFreeCacheSize, newEntryCodec(nil))
	if err := local.set("oversized", make([]byte, minimumFreeCacheSize), time.Minute); err == nil {
		t.Fatal("set() error = nil for entry larger than FreeCache's byte budget")
	}
}

func TestFreeCacheTTLSeconds(t *testing.T) {
	tests := []struct {
		name string
		ttl  time.Duration
		want int
	}{
		{"negative floor", -time.Second, 1},
		{"zero floor", 0, 1},
		{"sub-second floor", time.Millisecond, 1},
		{"whole second", time.Second, 1},
		{"round upward", time.Second + time.Nanosecond, 2},
		{"multiple seconds", 3 * time.Second, 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := freeCacheTTLSeconds(test.ttl); got != test.want {
				t.Fatalf("freeCacheTTLSeconds(%v) = %d, want %d", test.ttl, got, test.want)
			}
		})
	}
}

func TestLocalCacheConcurrentAccess(t *testing.T) {
	clock := &mutableClock{now: time.UnixMilli(1_700_000_000_000)}
	codec := newEntryCodec(clock)
	local := newLocalCache(8*minimumFreeCacheSize, codec)
	value := codec.inProgress(time.Hour)
	encoded, err := codec.encode(value)
	if err != nil {
		t.Fatalf("encode() error = %v", err)
	}

	const goroutines = 32
	const iterations = 100
	errors := make(chan error, goroutines)
	var workers sync.WaitGroup
	for worker := range goroutines {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for iteration := range iterations {
				key := fmt.Sprintf("key-%d-%d", worker, iteration)
				if err := local.set(key, encoded, time.Hour); err != nil {
					errors <- fmt.Errorf("set %s: %w", key, err)
					return
				}
				if got, found := local.get(key); !found || !got.InProgress {
					errors <- fmt.Errorf("get %s: found=%t entry=%#v", key, found, got)
					return
				}
			}
		}(worker)
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}
