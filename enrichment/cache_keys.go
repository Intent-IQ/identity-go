package enrichment

import (
	"strings"

	"github.com/prebid/openrtb/v20/openrtb2"
)

var sharedIDCacheSources = map[string]struct{}{
	"pubcid.org":   {},
	"sharedid.org": {},
}

// extractCacheKeys returns ordered, de-duplicated aliases for an auction.
// maxKeys values at or below zero retain the legacy unlimited behavior.
func extractCacheKeys(request *openrtb2.BidRequest, maxKeys int) []CacheKey {
	if request == nil {
		return nil
	}

	var eids []openrtb2.EID
	if request.User != nil {
		eids = request.User.EIDs
	}

	keys := appendMatchingEIDKeys(nil, eids, "iiq", CacheKeyThirdParty, func(source string) bool {
		return source == iiqSource
	})
	keys = appendMatchingEIDKeys(keys, eids, "pubcid", CacheKeyFirstParty, func(source string) bool {
		_, shared := sharedIDCacheSources[source]
		return shared
	})
	keys = appendMAIDKey(keys, request.Device)
	keys = appendOtherEIDKeys(keys, eids)
	keys = appendDeviceKey(keys, request.Device)

	return deduplicateAndCapKeys(keys, maxKeys)
}

func appendMatchingEIDKeys(
	keys []CacheKey,
	eids []openrtb2.EID,
	namespace string,
	keyType CacheKeyType,
	matches func(string) bool,
) []CacheKey {
	for _, eid := range eids {
		if eid.Source == "" || !matches(eid.Source) {
			continue
		}
		for _, uid := range eid.UIDs {
			if !notBlank(uid.ID) {
				continue
			}
			keys = append(keys, CacheKey{Value: namespace + ":" + uid.ID, Type: keyType})
		}
	}
	return keys
}

func appendMAIDKey(keys []CacheKey, device *openrtb2.Device) []CacheKey {
	if device == nil || !notBlank(device.IFA) || (device.Lmt != nil && *device.Lmt == 1) {
		return keys
	}

	ifa := device.IFA
	if device.DeviceType == 3 || device.DeviceType == 7 {
		ifa = strings.ToUpper(ifa)
	}
	return append(keys, CacheKey{Value: "maid:" + ifa, Type: CacheKeyFirstParty})
}

func appendOtherEIDKeys(keys []CacheKey, eids []openrtb2.EID) []CacheKey {
	for _, eid := range eids {
		if eid.Source == "" || eid.Source == iiqSource {
			continue
		}
		if _, shared := sharedIDCacheSources[eid.Source]; shared {
			continue
		}

		namespace := strings.ToLower(eid.Source)
		for _, uid := range eid.UIDs {
			if !notBlank(uid.ID) {
				continue
			}
			keys = append(keys, CacheKey{Value: namespace + ":" + uid.ID, Type: CacheKeyFirstParty})
		}
	}
	return keys
}

func appendDeviceKey(keys []CacheKey, device *openrtb2.Device) []CacheKey {
	if device == nil {
		return keys
	}

	ip := device.IP
	if !notBlank(ip) {
		ip = device.IPv6
	}
	fields := make([]string, 0, 3)
	for _, field := range []string{device.IFA, strings.TrimSpace(normalizeUserAgent(device.UA)), ip} {
		if notBlank(field) {
			fields = append(fields, field)
		}
	}
	if len(fields) == 0 {
		return keys
	}
	return append(keys, CacheKey{Value: "dev:" + strings.Join(fields, "_"), Type: CacheKeyDevice})
}

func deduplicateAndCapKeys(keys []CacheKey, maxKeys int) []CacheKey {
	if len(keys) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(keys))
	result := make([]CacheKey, 0, len(keys))
	for _, key := range keys {
		if _, duplicate := seen[key.Value]; duplicate {
			continue
		}
		seen[key.Value] = struct{}{}
		result = append(result, key)
		if maxKeys > 0 && len(result) >= maxKeys {
			break
		}
	}
	return result
}
