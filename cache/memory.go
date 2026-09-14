package cache

import (
	"math"
	"time"

	"github.com/coocood/freecache"
)

const minimumFreeCacheSize = 512 * 1024

// localCache is the internal, byte-bounded L1 cache. Absolute expiration is
// validated by the codec after every read in addition to FreeCache's own TTL.
type localCache struct {
	values   *freecache.Cache
	codec    entryCodec
	capacity int
}

func newLocalCache(maxSizeBytes int, codec entryCodec) *localCache {
	capacity := maxSizeBytes
	if capacity < minimumFreeCacheSize {
		capacity = minimumFreeCacheSize
	}
	return &localCache{
		values:   freecache.NewCache(capacity),
		codec:    codec,
		capacity: capacity,
	}
}

func (cache *localCache) get(key string) (entry, bool) {
	encoded, err := cache.values.Get([]byte(key))
	if err != nil {
		return entry{}, false
	}
	return cache.codec.decode(encoded)
}

func (cache *localCache) set(key string, encoded []byte, ttl time.Duration) error {
	return cache.values.Set([]byte(key), encoded, freeCacheTTLSeconds(ttl))
}

// freeCacheTTLSeconds rounds upward because FreeCache accepts only whole seconds.
// A one-second floor prevents sub-second or non-positive durations from becoming
// immediate expiration, matching the previous cache implementation.
func freeCacheTTLSeconds(ttl time.Duration) int {
	seconds := int(math.Ceil(ttl.Seconds()))
	if seconds < 1 {
		return 1
	}
	return seconds
}
