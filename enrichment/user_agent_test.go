package enrichment

import "testing"

func TestNormalizeUserAgent(t *testing.T) {
	tests := []struct {
		name      string
		userAgent string
		want      string
	}{
		{"blank", "", ""},
		{"whitespace only", "   ", ""},
		{"unrecognized", "SomeRandomBot/1.0", ""},
		{
			"iPhone Safari mobile",
			"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
			"iOS17_MobileSafari17_iPhone",
		},
		{
			"Android Chrome mobile",
			"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
			"Android14_MobileChrome120_Pixel8",
		},
		{
			"Windows desktop Chrome",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			"Windows10_Chrome120",
		},
		{
			"Mac Safari desktop",
			"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1 (KHTML, like Gecko) Version/17.0 Safari/605.1",
			"MacOSX10_Safari17_Mac",
		},
		{
			"Edge precedes Chrome",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
			"Windows10_Edge120",
		},
		{
			"Firefox",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
			"Windows10_Firefox121",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeUserAgent(test.userAgent); got != test.want {
				t.Fatalf("normalizeUserAgent() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeUserAgentIsDeterministic(t *testing.T) {
	userAgents := []string{
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) Version/17.0 Mobile Safari/604.1",
		"Mozilla/5.0 (Linux; Android 14; Pixel 8) Chrome/120.0.0.0 Mobile Safari/537.36",
		"garbage",
		"",
	}
	for _, userAgent := range userAgents {
		first := normalizeUserAgent(userAgent)
		for range 5 {
			if got := normalizeUserAgent(userAgent); got != first {
				t.Fatalf("normalizeUserAgent(%q) = %q, first result %q", userAgent, got, first)
			}
		}
	}
}
