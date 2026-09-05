package enrichment

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/prebid/openrtb/v20/openrtb2"
)

const highEntropyUAHintsSource = 2

// buildUAHints converts high-entropy OpenRTB structured UA data into the
// numeric-keyed format consumed by the IIQ S2S API.
func buildUAHints(userAgent *openrtb2.UserAgent) string {
	if userAgent == nil || int(userAgent.Source) != highEntropyUAHintsSource {
		return ""
	}

	hints := make(map[string]string)
	appendBrowserHints(hints, userAgent.Browsers)
	if userAgent.Mobile != nil {
		hints["1"] = "?" + strconv.Itoa(int(*userAgent.Mobile))
	}
	appendPlatformHints(hints, userAgent.Platform)
	putQuotedHint(hints, "3", userAgent.Architecture)
	putQuotedHint(hints, "4", userAgent.Bitness)
	putQuotedHint(hints, "5", userAgent.Model)
	if len(hints) == 0 {
		return ""
	}

	encoded, err := json.Marshal(hints)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// appendBrowserHints adds major versions under key 0 and full versions under key 8.
func appendBrowserHints(hints map[string]string, browsers []openrtb2.BrandVersion) {
	majorVersions := make(map[string]string)
	fullVersions := make(map[string]string)
	for _, browser := range browsers {
		if !notBlank(browser.Brand) || len(browser.Version) == 0 {
			continue
		}

		fullVersion := strings.Join(browser.Version, ".")
		majorVersion := fullVersion
		if separator := strings.IndexByte(fullVersion, '.'); separator > 0 {
			majorVersion = fullVersion[:separator]
		}
		majorVersions[browser.Brand] = majorVersion
		fullVersions[browser.Brand] = fullVersion
	}
	putBrandList(hints, "0", majorVersions)
	putBrandList(hints, "8", fullVersions)
}

func putBrandList(hints map[string]string, key string, versions map[string]string) {
	if len(versions) == 0 {
		return
	}

	brands := make([]string, 0, len(versions))
	for brand := range versions {
		brands = append(brands, brand)
	}
	sort.Strings(brands)

	values := make([]string, 0, len(brands))
	for _, brand := range brands {
		values = append(values, quoteHint(brand)+";v="+quoteHint(versions[brand]))
	}
	hints[key] = strings.Join(values, ", ")
}

// appendPlatformHints adds the platform brand under key 2 and version under key 6.
func appendPlatformHints(hints map[string]string, platform *openrtb2.BrandVersion) {
	if platform == nil || !notBlank(platform.Brand) {
		return
	}
	hints["2"] = quoteHint(platform.Brand)
	if len(platform.Version) > 0 {
		hints["6"] = quoteHint(strings.Join(platform.Version, "."))
	}
}

func putQuotedHint(hints map[string]string, key, value string) {
	if notBlank(value) {
		hints[key] = quoteHint(value)
	}
}

func quoteHint(value string) string {
	return `"` + value + `"`
}
