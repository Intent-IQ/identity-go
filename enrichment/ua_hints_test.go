package enrichment

import (
	"encoding/json"
	"testing"

	"github.com/prebid/openrtb/v20/openrtb2"
)

func TestBuildUAHintsFromDeviceSUA(t *testing.T) {
	mobile := int8(0)
	userAgent := &openrtb2.UserAgent{
		Source: 2,
		Browsers: []openrtb2.BrandVersion{
			{Brand: "Chromium", Version: []string{"108", "0", "5359", "125"}},
			{Brand: "Google Chrome", Version: []string{"108", "0", "5359", "125"}},
			{Brand: "Not?A_Brand", Version: []string{"8", "0", "0", "0"}},
		},
		Platform:     &openrtb2.BrandVersion{Brand: "Windows", Version: []string{"15", "0", "0"}},
		Mobile:       &mobile,
		Architecture: "x86",
		Bitness:      "64",
	}

	encoded := buildUAHints(userAgent)
	wantEncoded := `{"0":"\"Chromium\";v=\"108\", \"Google Chrome\";v=\"108\", \"Not?A_Brand\";v=\"8\"","1":"?0","2":"\"Windows\"","3":"\"x86\"","4":"\"64\"","6":"\"15.0.0\"","8":"\"Chromium\";v=\"108.0.5359.125\", \"Google Chrome\";v=\"108.0.5359.125\", \"Not?A_Brand\";v=\"8.0.0.0\""}`
	assertEqual(t, wantEncoded, encoded)

	var hints map[string]string
	if err := json.Unmarshal([]byte(encoded), &hints); err != nil {
		t.Fatalf("buildUAHints() returned invalid JSON: %v", err)
	}
	assertEqual(t, `"Chromium";v="108", "Google Chrome";v="108", "Not?A_Brand";v="8"`, hints["0"])
	assertEqual(t, `"Chromium";v="108.0.5359.125", "Google Chrome";v="108.0.5359.125", "Not?A_Brand";v="8.0.0.0"`, hints["8"])
	assertEqual(t, "?0", hints["1"])
	assertEqual(t, `"Windows"`, hints["2"])
	assertEqual(t, `"x86"`, hints["3"])
	assertEqual(t, `"64"`, hints["4"])
	assertEqual(t, `"15.0.0"`, hints["6"])
	if _, exists := hints["5"]; exists {
		t.Fatal("model key 5 unexpectedly present")
	}
	if _, exists := hints["7"]; exists {
		t.Fatal("reserved key 7 unexpectedly present")
	}
}

func TestBuildUAHintsPlacesModelUnderKeyFive(t *testing.T) {
	var hints map[string]string
	if err := json.Unmarshal([]byte(buildUAHints(&openrtb2.UserAgent{Source: 2, Model: "Pixel 7"})), &hints); err != nil {
		t.Fatalf("buildUAHints() returned invalid JSON: %v", err)
	}
	assertEqual(t, `"Pixel 7"`, hints["5"])
}

func TestBuildUAHintsOmitsLowEntropyAndEmptyValues(t *testing.T) {
	tests := []struct {
		name      string
		userAgent *openrtb2.UserAgent
	}{
		{"nil", nil},
		{"low entropy", &openrtb2.UserAgent{Source: 1, Model: "Pixel"}},
		{"no data", &openrtb2.UserAgent{Source: 2}},
		{"blank data", &openrtb2.UserAgent{
			Source:       2,
			Browsers:     []openrtb2.BrandVersion{{Brand: " ", Version: []string{"120"}}, {Brand: "Chrome"}},
			Platform:     &openrtb2.BrandVersion{Brand: " ", Version: []string{"15"}},
			Architecture: " ", Bitness: " ", Model: " ",
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertEqual(t, "", buildUAHints(test.userAgent))
		})
	}
}

func TestBuildS2SRequestPlacesUAHintsBeforeDeviceID(t *testing.T) {
	requestURL, _ := buildS2SRequest(Request{
		Endpoint:  testEndpoint,
		PartnerID: "123",
		Auction: &openrtb2.BidRequest{Device: &openrtb2.Device{
			SUA: &openrtb2.UserAgent{Source: 2, Model: "Pixel"},
			IFA: "maid-1",
		}},
	})

	want := testEndpoint + "?at=39&mi=10&dpi=123&pt=17&dpn=1&srvrReq=true&source=pbsgo" +
		"&uh=%7B%225%22%3A%22%5C%22Pixel%5C%22%22%7D&pcid=maid-1&idtype=4"
	assertEqual(t, want, requestURL)
}

func TestBuildS2SRequestOmitsLowEntropyUAHints(t *testing.T) {
	requestURL, _ := buildS2SRequest(Request{
		Endpoint: testEndpoint,
		Auction: &openrtb2.BidRequest{Device: &openrtb2.Device{
			SUA: &openrtb2.UserAgent{Source: 1, Model: "Pixel"},
		}},
	})
	assertNotContains(t, requestURL, "&uh=")
}
