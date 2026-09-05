package enrichment

import "testing"

func TestCacheModelTokens(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"state miss", CacheMiss.Token(), "miss"},
		{"state hit", CacheHit.Token(), "hit"},
		{"state negative", CacheNegative.Token(), "negative"},
		{"state in progress", CacheInProgress.Token(), "in_progress"},
		{"state unknown", CacheState(99).Token(), "unknown"},
		{"layer none", CacheLayerNone.Token(), "none"},
		{"layer l1", CacheLayerL1.Token(), "l1"},
		{"layer l2", CacheLayerL2.Token(), "l2"},
		{"layer unknown", CacheLayer(99).Token(), "none"},
		{"key first party", CacheKeyFirstParty.Token(), "first_party"},
		{"key third party", CacheKeyThirdParty.Token(), "third_party"},
		{"key device", CacheKeyDevice.Token(), "device"},
		{"key unknown", CacheKeyType(99).Token(), "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("token = %q, want %q", test.got, test.want)
			}
		})
	}
}
