package cache

import (
	"reflect"
	"testing"
	"time"

	"github.com/Intent-IQ/identity-go/enrichment"
	"github.com/prebid/openrtb/v20/openrtb2"
)

const fixtureExpiry = int64(1_700_000_060_000)

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

func TestEntryCodecDecodesOldFixtures(t *testing.T) {
	terminationCause := int64(120088)
	codec := newEntryCodec(fixedClock{now: time.UnixMilli(1_700_000_000_000)})
	tests := []struct {
		name    string
		fixture string
		want    entry
	}{
		{
			"positive",
			`{"eids":[{"source":"intentiq.com","uids":[{"id":"uid-1"}]}],"abTestUuid":"ab-1","tc":120088,"exp":1700000060000}`,
			entry{EIDs: []openrtb2.EID{{Source: "intentiq.com", UIDs: []openrtb2.UID{{ID: "uid-1"}}}}, ABTestUUID: "ab-1", TerminationCause: &terminationCause, ExpiresAt: fixtureExpiry},
		},
		{
			"negative",
			`{"abTestUuid":"ab-1","tc":120088,"negative":true,"exp":1700000060000}`,
			entry{ABTestUUID: "ab-1", TerminationCause: &terminationCause, Negative: true, ExpiresAt: fixtureExpiry},
		},
		{
			"in progress",
			`{"inProgress":true,"exp":1700000060000}`,
			entry{InProgress: true, ExpiresAt: fixtureExpiry},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, valid := codec.decode([]byte(test.fixture))
			if !valid {
				t.Fatal("decode() valid = false, want true")
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("decode() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestEntryCodecRetainsCanonicalWireFormat(t *testing.T) {
	terminationCause := int64(120088)
	codec := newEntryCodec(fixedClock{now: time.UnixMilli(1_700_000_000_000)})
	tests := []struct {
		name string
		got  entry
		want string
	}{
		{
			"positive",
			codec.resolved(enrichment.Result{EIDs: []openrtb2.EID{{Source: "intentiq.com", UIDs: []openrtb2.UID{{ID: "uid-1"}}}}, ABTestUUID: "ab-1", TerminationCause: &terminationCause}, time.Minute),
			`{"eids":[{"source":"intentiq.com","uids":[{"id":"uid-1"}]}],"abTestUuid":"ab-1","tc":120088,"exp":1700000060000}`,
		},
		{
			"negative",
			codec.negative(enrichment.ResultMetadata{ABTestUUID: "ab-1", TerminationCause: &terminationCause}, time.Minute),
			`{"abTestUuid":"ab-1","tc":120088,"negative":true,"exp":1700000060000}`,
		},
		{
			"in progress",
			codec.inProgress(time.Minute),
			`{"inProgress":true,"exp":1700000060000}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := codec.encode(test.got)
			if err != nil {
				t.Fatalf("encode() error = %v", err)
			}
			if string(encoded) != test.want {
				t.Fatalf("encode() = %s, want %s", encoded, test.want)
			}
		})
	}
}

func TestEntryCodecRejectsInvalidAndExpiredEntries(t *testing.T) {
	codec := newEntryCodec(fixedClock{now: time.UnixMilli(1_700_000_000_000)})
	tests := []struct {
		name  string
		value []byte
	}{
		{"empty", nil},
		{"malformed", []byte(`{bad`)},
		{"before boundary", []byte(`{"exp":1699999999999}`)},
		{"exact boundary", []byte(`{"exp":1700000000000}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, valid := codec.decode(test.value); valid {
				t.Fatal("decode() valid = true, want false")
			}
		})
	}

	if _, valid := codec.decode([]byte(`{"exp":1700000000001}`)); !valid {
		t.Fatal("decode() rejected entry one millisecond after boundary")
	}
}

func TestCacheResultFromEntry(t *testing.T) {
	terminationCause := int64(7)
	tests := []struct {
		name  string
		entry entry
		state enrichment.CacheState
	}{
		{"hit", entry{EIDs: []openrtb2.EID{{Source: "intentiq.com"}}, ABTestUUID: "ab", TerminationCause: &terminationCause}, enrichment.CacheHit},
		{"negative", entry{Negative: true, ABTestUUID: "ab", TerminationCause: &terminationCause}, enrichment.CacheNegative},
		{"in progress takes precedence", entry{Negative: true, InProgress: true, ABTestUUID: "ignored", TerminationCause: &terminationCause}, enrichment.CacheInProgress},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := cacheResultFromEntry(test.entry, enrichment.CacheKeyThirdParty, enrichment.CacheLayerL2)
			if got.State != test.state || got.KeyType != enrichment.CacheKeyThirdParty || got.Layer != enrichment.CacheLayerL2 {
				t.Fatalf("cacheResultFromEntry() = %#v", got)
			}
			if test.state == enrichment.CacheInProgress {
				if got.Result.ABTestUUID != "" || got.Result.TerminationCause != nil {
					t.Fatalf("in-progress result carries metadata: %#v", got.Result)
				}
			} else if got.Result.ABTestUUID != "ab" || got.Result.TerminationCause != &terminationCause {
				t.Fatalf("resolution metadata was not preserved: %#v", got.Result)
			}
		})
	}
}
