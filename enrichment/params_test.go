package enrichment

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/prebid/openrtb/v20/adcom1"
	"github.com/prebid/openrtb/v20/openrtb2"
)

const testEndpoint = "https://dev.example.com/resolve"

func TestBuildS2SRequestFixedParameters(t *testing.T) {
	requestURL, consent := buildS2SRequest(Request{
		Endpoint:  testEndpoint,
		PartnerID: "partner-42",
		Auction:   &openrtb2.BidRequest{},
	})

	want := testEndpoint + "?at=39&mi=10&dpi=partner-42&pt=17&dpn=1&srvrReq=true&source=pbsgo"
	assertEqual(t, want, requestURL)
	assertEqual(t, "", consent)
}

func TestBuildS2SRequestPreservesExistingQuery(t *testing.T) {
	requestURL, _ := buildS2SRequest(Request{
		Endpoint:  testEndpoint + "?x=1",
		PartnerID: "p",
		Auction:   &openrtb2.BidRequest{},
	})

	want := testEndpoint + "?x=1&at=39&mi=10&dpi=p&pt=17&dpn=1&srvrReq=true&source=pbsgo"
	assertEqual(t, want, requestURL)
}

func TestBuildS2SRequestMapsDeviceInOrder(t *testing.T) {
	requestURL, _ := buildS2SRequest(Request{
		Endpoint:  testEndpoint,
		PartnerID: "383342646",
		Auction: &openrtb2.BidRequest{Device: &openrtb2.Device{
			IP: "125.253.50.47", IPv6: "2001:db8::1", UA: "Mozilla/5.0 (iPhone)",
			IFA: "maid-AbC", DeviceType: 1,
		}},
	})

	want := testEndpoint + "?at=39&mi=10&dpi=383342646&pt=17&dpn=1&srvrReq=true&source=pbsgo" +
		"&ip=125.253.50.47&ipv6=2001%3Adb8%3A%3A1&uas=Mozilla%2F5.0%20%28iPhone%29" +
		"&pcid=maid-AbC&idtype=4"
	assertEqual(t, want, requestURL)
}

func TestBuildS2SRequestMapsCTVDeviceIDs(t *testing.T) {
	for _, deviceType := range []adcom1.DeviceType{3, 7} {
		t.Run(string(rune('0'+deviceType)), func(t *testing.T) {
			requestURL, _ := buildS2SRequest(Request{
				Endpoint: testEndpoint,
				Auction: &openrtb2.BidRequest{Device: &openrtb2.Device{
					IFA: "rida-AbC", DeviceType: deviceType,
				}},
			})
			assertContains(t, requestURL, "&pcid=RIDA-ABC&idtype=8")
		})
	}
}

func TestBuildS2SRequestSuppressesUnavailableDeviceID(t *testing.T) {
	limited := int8(1)
	tests := []openrtb2.Device{
		{IFA: "   "},
		{IFA: "maid-1", Lmt: &limited},
	}
	for _, device := range tests {
		requestURL, _ := buildS2SRequest(Request{Endpoint: testEndpoint, Auction: &openrtb2.BidRequest{Device: &device}})
		assertNotContains(t, requestURL, "pcid=")
		assertNotContains(t, requestURL, "idtype=")
	}
}

func TestBuildS2SRequestMapsReferrerPrecedence(t *testing.T) {
	tests := []struct {
		name    string
		request *openrtb2.BidRequest
		want    string
	}{
		{"site domain", &openrtb2.BidRequest{Site: &openrtb2.Site{Domain: "example.com", Page: "https://example.com/p"}}, "&ref=example.com"},
		{"site page", &openrtb2.BidRequest{Site: &openrtb2.Site{Page: "https://example.com/p"}}, "&ref=https%3A%2F%2Fexample.com%2Fp"},
		{"app bundle", &openrtb2.BidRequest{App: &openrtb2.App{Bundle: "com.x.y", Name: "App"}}, "&ref=com.x.y"},
		{"app name", &openrtb2.BidRequest{App: &openrtb2.App{Name: "MyApp"}}, "&ref=MyApp"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestURL, _ := buildS2SRequest(Request{Endpoint: testEndpoint, Auction: test.request})
			assertContains(t, requestURL, test.want)
		})
	}

	requestURL, _ := buildS2SRequest(Request{Endpoint: testEndpoint, Auction: &openrtb2.BidRequest{
		Site: &openrtb2.Site{}, App: &openrtb2.App{Bundle: "ignored.app"},
	}})
	assertNotContains(t, requestURL, "ref=")
}

func TestBuildS2SRequestMapsFirstNonBlankIIQUID(t *testing.T) {
	requestURL, _ := buildS2SRequest(Request{Endpoint: testEndpoint, Auction: &openrtb2.BidRequest{
		User: &openrtb2.User{EIDs: []openrtb2.EID{
			{Source: "other.com", UIDs: []openrtb2.UID{{ID: "other"}}},
			{Source: iiqSource, UIDs: []openrtb2.UID{{ID: " "}, {ID: "IIQ-UID-1"}, {ID: "IIQ-UID-2"}}},
		}},
	}})
	assertContains(t, requestURL, "&iiquid=IIQ-UID-1")
	assertNotContains(t, requestURL, "IIQ-UID-2")
}

func TestBuildS2SRequestMapsTopLevelPrivacyAndConsent(t *testing.T) {
	gdpr := int8(1)
	requestURL, consent := buildS2SRequest(Request{Endpoint: testEndpoint, Auction: &openrtb2.BidRequest{
		Regs: &openrtb2.Regs{GDPR: &gdpr, USPrivacy: "1YNN", GPP: "DBABMA~CONSENT", GPPSID: []int8{2, 6}},
		User: &openrtb2.User{Consent: "CO-TCF-STRING"},
	}})

	assertContains(t, requestURL, "&gdpr=1&us_privacy=1YNN&gpp=DBABMA~CONSENT&gpp_sid=2%2C6")
	assertNotContains(t, requestURL, "CO-TCF-STRING")
	assertEqual(t, "CO-TCF-STRING", consent)
}

func TestBuildS2SRequestMapsExtensionFallbacks(t *testing.T) {
	requestURL, consent := buildS2SRequest(Request{Endpoint: testEndpoint, Auction: &openrtb2.BidRequest{
		Regs: &openrtb2.Regs{Ext: json.RawMessage(`{"gdpr":1,"us_privacy":"1NYN"}`)},
		User: &openrtb2.User{Ext: json.RawMessage(`{"consent":"EXT-TCF-STRING"}`)},
	}})

	assertContains(t, requestURL, "&gdpr=1&us_privacy=1NYN")
	assertEqual(t, "EXT-TCF-STRING", consent)
}

func TestBuildS2SRequestIgnoresMalformedExtensions(t *testing.T) {
	tests := []struct {
		name string
		regs json.RawMessage
		user json.RawMessage
	}{
		{"malformed", json.RawMessage(`{"gdpr":`), json.RawMessage(`{"consent":`)},
		{"wrong types", json.RawMessage(`{"gdpr":"yes","us_privacy":1}`), json.RawMessage(`{"consent":1}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestURL, consent := buildS2SRequest(Request{Endpoint: testEndpoint, Auction: &openrtb2.BidRequest{
				Regs: &openrtb2.Regs{Ext: test.regs}, User: &openrtb2.User{Ext: test.user},
			}})
			assertNotContains(t, requestURL, "&gdpr=")
			assertNotContains(t, requestURL, "&us_privacy=")
			assertEqual(t, "", consent)
		})
	}
}

func TestBuildS2SRequestHandlesNilAndBlankValues(t *testing.T) {
	want := testEndpoint + "?at=39&mi=10&pt=17&dpn=1&srvrReq=true&source=pbsgo"
	for _, auction := range []*openrtb2.BidRequest{nil, {}, {Device: &openrtb2.Device{}, User: &openrtb2.User{}, Regs: &openrtb2.Regs{}}} {
		requestURL, consent := buildS2SRequest(Request{Endpoint: testEndpoint, PartnerID: "   ", Auction: auction})
		assertEqual(t, want, requestURL)
		assertEqual(t, "", consent)
	}
}

func assertEqual(t *testing.T, want, got string) {
	t.Helper()
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func assertContains(t *testing.T, value, substring string) {
	t.Helper()
	if !strings.Contains(value, substring) {
		t.Fatalf("%q does not contain %q", value, substring)
	}
}

func assertNotContains(t *testing.T, value, substring string) {
	t.Helper()
	if strings.Contains(value, substring) {
		t.Fatalf("%q unexpectedly contains %q", value, substring)
	}
}
