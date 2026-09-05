package enrichment

import (
	"reflect"
	"testing"

	"github.com/prebid/openrtb/v20/openrtb2"
)

func TestExtractCacheKeys(t *testing.T) {
	tests := []struct {
		name    string
		maxKeys int
		request *openrtb2.BidRequest
		want    []CacheKey
	}{
		{"nil request", 10, nil, nil},
		{
			"priority order iiq pubcid maid other device",
			10,
			&openrtb2.BidRequest{
				User: &openrtb2.User{EIDs: []openrtb2.EID{
					cacheEID("intentiq.com", "iiqid"), cacheEID("pubcid.org", "pub1"), cacheEID("uidapi.com", "uid2"),
				}},
				Device: &openrtb2.Device{IFA: "ifa-1", IP: "1.2.3.4"},
			},
			[]CacheKey{
				{Value: "iiq:iiqid", Type: CacheKeyThirdParty},
				{Value: "pubcid:pub1", Type: CacheKeyFirstParty},
				{Value: "maid:ifa-1", Type: CacheKeyFirstParty},
				{Value: "uidapi.com:uid2", Type: CacheKeyFirstParty},
				{Value: "dev:ifa-1_1.2.3.4", Type: CacheKeyDevice},
			},
		},
		{"sharedid uses pubcid namespace", 10, requestWithEIDs(cacheEID("sharedid.org", "s1")), []CacheKey{{Value: "pubcid:s1", Type: CacheKeyFirstParty}}},
		{"blank uids omitted", 10, requestWithEIDs(cacheEID("intentiq.com", "  ", "real")), []CacheKey{{Value: "iiq:real", Type: CacheKeyThirdParty}}},
		{
			"other source namespace lowercased after case-sensitive matching",
			10,
			requestWithEIDs(cacheEID("UIDAPI.COM", "uid"), cacheEID("INTENTIQ.COM", "upper-iiq")),
			[]CacheKey{{Value: "uidapi.com:uid", Type: CacheKeyFirstParty}, {Value: "intentiq.com:upper-iiq", Type: CacheKeyFirstParty}},
		},
		{
			"deduplication retains first occurrence",
			10,
			requestWithEIDs(cacheEID("uidapi.com", "dup"), cacheEID("UIDAPI.COM", "dup")),
			[]CacheKey{{Value: "uidapi.com:dup", Type: CacheKeyFirstParty}},
		},
		{
			"positive max keys cap",
			2,
			requestWithEIDs(cacheEID("intentiq.com", "a"), cacheEID("pubcid.org", "b"), cacheEID("uidapi.com", "c")),
			[]CacheKey{{Value: "iiq:a", Type: CacheKeyThirdParty}, {Value: "pubcid:b", Type: CacheKeyFirstParty}},
		},
		{"no identifiers", 10, &openrtb2.BidRequest{}, nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := extractCacheKeys(test.request, test.maxKeys)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("extractCacheKeys() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestExtractCacheKeysDeviceBehavior(t *testing.T) {
	limited := int8(1)
	tests := []struct {
		name   string
		device *openrtb2.Device
		want   []CacheKey
	}{
		{
			"LMT suppresses MAID but retains composite IFA",
			&openrtb2.Device{IFA: "ifa-x", Lmt: &limited},
			[]CacheKey{{Value: "dev:ifa-x", Type: CacheKeyDevice}},
		},
		{
			"CTV type 3 uppercases only MAID",
			&openrtb2.Device{IFA: "abc-DEF", DeviceType: 3},
			[]CacheKey{{Value: "maid:ABC-DEF", Type: CacheKeyFirstParty}, {Value: "dev:abc-DEF", Type: CacheKeyDevice}},
		},
		{
			"CTV type 7 uppercases only MAID",
			&openrtb2.Device{IFA: "abc-DEF", DeviceType: 7},
			[]CacheKey{{Value: "maid:ABC-DEF", Type: CacheKeyFirstParty}, {Value: "dev:abc-DEF", Type: CacheKeyDevice}},
		},
		{"IPv6 fallback", &openrtb2.Device{IPv6: "::1"}, []CacheKey{{Value: "dev:::1", Type: CacheKeyDevice}}},
		{"blank IPv4 uses IPv6", &openrtb2.Device{IP: "  ", IPv6: "::2"}, []CacheKey{{Value: "dev:::2", Type: CacheKeyDevice}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := extractCacheKeys(&openrtb2.BidRequest{Device: test.device}, 10)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("extractCacheKeys() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestExtractCacheKeysUsesNormalizedUserAgent(t *testing.T) {
	userAgent := "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
	request := &openrtb2.BidRequest{Device: &openrtb2.Device{IFA: "ifa", UA: userAgent, IP: "9.9.9.9"}}
	want := []CacheKey{
		{Value: "maid:ifa", Type: CacheKeyFirstParty},
		{Value: "dev:ifa_iOS17_MobileSafari17_iPhone_9.9.9.9", Type: CacheKeyDevice},
	}
	if got := extractCacheKeys(request, 10); !reflect.DeepEqual(got, want) {
		t.Fatalf("extractCacheKeys() = %#v, want %#v", got, want)
	}
}

func TestExtractCacheKeysNonPositiveLimitIsUnlimited(t *testing.T) {
	request := requestWithEIDs(cacheEID("intentiq.com", "a"), cacheEID("pubcid.org", "b"), cacheEID("uidapi.com", "c"))
	want := extractCacheKeys(request, 10)
	for _, maxKeys := range []int{0, -1} {
		if got := extractCacheKeys(request, maxKeys); !reflect.DeepEqual(got, want) {
			t.Fatalf("extractCacheKeys(maxKeys=%d) = %#v, want %#v", maxKeys, got, want)
		}
	}
}

func cacheEID(source string, ids ...string) openrtb2.EID {
	uids := make([]openrtb2.UID, 0, len(ids))
	for _, id := range ids {
		uids = append(uids, openrtb2.UID{ID: id})
	}
	return openrtb2.EID{Source: source, UIDs: uids}
}

func requestWithEIDs(eids ...openrtb2.EID) *openrtb2.BidRequest {
	return &openrtb2.BidRequest{User: &openrtb2.User{EIDs: eids}}
}
