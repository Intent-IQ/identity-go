package enrichment

import (
	"regexp"
	"strings"
)

// normalizeUserAgent produces the compact, deterministic segment used by device cache keys.
func normalizeUserAgent(userAgent string) string {
	userAgent = strings.TrimSpace(userAgent)
	if userAgent == "" {
		return ""
	}

	parts := make([]string, 0, 3)
	if value := matchUserAgentRules(userAgent, operatingSystemRules); value != "" {
		parts = append(parts, value)
	}
	if value := browserToken(userAgent); value != "" {
		parts = append(parts, value)
	}
	if value := matchUserAgentRules(userAgent, deviceRules); value != "" {
		parts = append(parts, value)
	}
	return strings.Join(parts, "_")
}

type userAgentRule struct {
	pattern *regexp.Regexp
	family  string
	version bool
}

var operatingSystemRules = []userAgentRule{
	{regexp.MustCompile(`(?i)(?:iphone os|cpu os)\s+(\d+)`), "iOS", true},
	{regexp.MustCompile(`(?i)mac os x\s+(\d+)`), "MacOSX", true},
	{regexp.MustCompile(`(?i)android[\s/]+(\d+)`), "Android", true},
	{regexp.MustCompile(`(?i)windows nt\s+(\d+)`), "Windows", true},
	{regexp.MustCompile(`(?i)cros`), "ChromeOS", false},
	{regexp.MustCompile(`(?i)linux`), "Linux", false},
}

var browserRules = []userAgentRule{
	{regexp.MustCompile(`(?i)edg(?:e|ios|a)?/(\d+)`), "Edge", true},
	{regexp.MustCompile(`(?i)(?:opr|opera)/(\d+)`), "Opera", true},
	{regexp.MustCompile(`(?i)samsungbrowser/(\d+)`), "SamsungBrowser", true},
	{regexp.MustCompile(`(?i)(?:firefox|fxios)/(\d+)`), "Firefox", true},
	{regexp.MustCompile(`(?i)(?:crios|chromium|chrome)/(\d+)`), "Chrome", true},
	{regexp.MustCompile(`(?i)version/(\d+).*safari`), "Safari", true},
}

var deviceRules = []userAgentRule{
	{regexp.MustCompile(`(?i)ipad`), "iPad", false},
	{regexp.MustCompile(`(?i)ipod`), "iPod", false},
	{regexp.MustCompile(`(?i)iphone`), "iPhone", false},
	{regexp.MustCompile(`(?i)pixel\s+(\d+)`), "Pixel", true},
	{regexp.MustCompile(`(?i)nexus\s+(\d+)`), "Nexus", true},
	{regexp.MustCompile(`(?i)macintosh`), "Mac", false},
}

func browserToken(userAgent string) string {
	mobile := strings.Contains(strings.ToLower(userAgent), "mobile")
	for _, rule := range browserRules {
		matches := rule.pattern.FindStringSubmatch(userAgent)
		if matches == nil {
			continue
		}
		family := rule.family
		if mobile && (family == "Chrome" || family == "Safari") {
			family = "Mobile" + family
		}
		return userAgentToken(family, matchedVersion(rule, matches))
	}
	return ""
}

func matchUserAgentRules(userAgent string, rules []userAgentRule) string {
	for _, rule := range rules {
		matches := rule.pattern.FindStringSubmatch(userAgent)
		if matches != nil {
			return userAgentToken(rule.family, matchedVersion(rule, matches))
		}
	}
	return ""
}

func matchedVersion(rule userAgentRule, matches []string) string {
	if rule.version && len(matches) > 1 {
		return matches[1]
	}
	return ""
}

var userAgentWhitespace = regexp.MustCompile(`\s+`)

func userAgentToken(family, version string) string {
	return userAgentWhitespace.ReplaceAllString(family+version, "")
}
