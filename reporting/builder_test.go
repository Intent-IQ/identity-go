package reporting

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/prebid/openrtb/v20/openrtb2"
)

func TestBuildReportURL(t *testing.T) {
	terminationCause := int64(0)
	request := Request{
		PartnerID:  "partner +&",
		Endpoint:   "https://reports.example/path?existing=yes",
		BidderCode: "bidder A",
		Currency:   "EUR",
		Bid: openrtb2.Bid{
			ImpID: "imp/1",
			Price: 1.5,
			Ext:   json.RawMessage(`{"origbidcpm":2.20,"origbidcur":" USD "}`),
		},
		AuctionID:        "auction&1",
		Reference:        " example.com/path ",
		IP:               "192.0.2.1",
		UserAgent:        "UA/1 + test",
		ABTestUUID:       "ab-1",
		TerminationCause: &terminationCause,
	}

	rdata := `{"bidderCode":"bidder A","partnerId":"partner +\u0026","cpm":1.5,"currency":"EUR","originalCpm":2.20,"originalCurrency":" USD ","placementId":"imp/1","biddingPlatformId":"4","vrref":" example.com/path ","prebidAuctionId":"auction\u00261","partnerAuctionId":"auction\u00261","abTestUuid":"ab-1","terminationCause":0,"ip":"192.0.2.1","ua":"UA/1 + test"}`
	want := "https://reports.example/path?existing=yes&at=45&rtype=1&source=pbsgo&dpi=" + encodeComponent(request.PartnerID) + "&rdata=" + encodeComponent(rdata)
	if got := buildReportURL(request); got != want {
		t.Fatalf("unexpected report URL:\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildReportURLDefaultsCurrencyAndOmitsBlankMetadata(t *testing.T) {
	request := Request{
		PartnerID:  "partner",
		Endpoint:   "https://reports.example/path",
		BidderCode: "bidder",
		Bid:        openrtb2.Bid{ImpID: "imp", Price: 2},
		AuctionID:  " \t\n",
		Reference:  " ",
		IP:         "\t",
		UserAgent:  "\n",
		ABTestUUID: "  ",
	}

	rdata := `{"bidderCode":"bidder","partnerId":"partner","cpm":2,"currency":"USD","placementId":"imp","biddingPlatformId":"4"}`
	want := "https://reports.example/path?at=45&rtype=1&source=pbsgo&dpi=partner&rdata=" + encodeComponent(rdata)
	if got := buildReportURL(request); got != want {
		t.Fatalf("unexpected report URL:\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildReportURLRetainsWhitespaceCurrency(t *testing.T) {
	request := Request{Endpoint: "endpoint", Currency: " ", Bid: openrtb2.Bid{}}
	decoded := decodeRData(t, buildReportURL(request))
	if got := decoded["currency"]; got != " " {
		t.Fatalf("currency = %#v, want one space", got)
	}
}

func TestAppendOriginalBid(t *testing.T) {
	tests := []struct {
		name                 string
		ext                  json.RawMessage
		wantOriginalCPM      string
		wantOriginalCurrency string
	}{
		{name: "valid", ext: json.RawMessage(`{"origbidcpm":1.25,"origbidcur":"EUR"}`), wantOriginalCPM: "1.25", wantOriginalCurrency: "EUR"},
		{name: "zero", ext: json.RawMessage(`{"origbidcpm":0,"origbidcur":"USD"}`), wantOriginalCPM: "0", wantOriginalCurrency: "USD"},
		{name: "quoted numeric cpm", ext: json.RawMessage(`{"origbidcpm":"2.5"}`), wantOriginalCPM: "2.5"},
		{name: "blank currency", ext: json.RawMessage(`{"origbidcpm":2,"origbidcur":" \t"}`), wantOriginalCPM: "2"},
		{name: "blank values", ext: json.RawMessage(`{"origbidcpm":"","origbidcur":""}`)},
		{name: "non numeric cpm", ext: json.RawMessage(`{"origbidcpm":"invalid","origbidcur":"GBP"}`), wantOriginalCurrency: "GBP"},
		{name: "wrong types", ext: json.RawMessage(`{"origbidcpm":true,"origbidcur":4}`)},
		{name: "missing fields", ext: json.RawMessage(`{"other":1}`)},
		{name: "malformed", ext: json.RawMessage(`{"origbidcpm":`)},
		{name: "empty"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := Request{Endpoint: "endpoint", Bid: openrtb2.Bid{Ext: test.ext}}
			rdata := decodeRData(t, buildReportURL(request))
			assertJSONNumber(t, rdata, "originalCpm", test.wantOriginalCPM)
			if got, present := rdata["originalCurrency"]; test.wantOriginalCurrency == "" {
				if present {
					t.Fatalf("originalCurrency unexpectedly present: %#v", got)
				}
			} else if got != test.wantOriginalCurrency {
				t.Fatalf("originalCurrency = %#v, want %q", got, test.wantOriginalCurrency)
			}
		})
	}
}

func TestOrderedMapMarshalJSON(t *testing.T) {
	ordered := newOrderedMap()
	ordered.put("b", 1)
	ordered.put("a", "x")
	ordered.put("b", 2)

	encoded, err := ordered.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	if got, want := string(encoded), `{"b":2,"a":"x"}`; got != want {
		t.Fatalf("MarshalJSON() = %s, want %s", got, want)
	}
}

func TestOrderedMapMarshalError(t *testing.T) {
	ordered := newOrderedMap()
	ordered.put("unsupported", func() {})
	if _, err := ordered.MarshalJSON(); err == nil {
		t.Fatal("MarshalJSON() error = nil, want an error")
	}
}

func TestAssembleReportURLIgnoresRDataMarshalError(t *testing.T) {
	rdata := newOrderedMap()
	rdata.put("unsupported", func() {})

	want := "endpoint?at=45&rtype=1&source=pbsgo&dpi=partner&rdata="
	if got := assembleReportURL("endpoint", "partner", rdata); got != want {
		t.Fatalf("assembleReportURL() = %q, want %q", got, want)
	}
}

func decodeRData(t *testing.T, reportURL string) map[string]any {
	t.Helper()
	marker := "&rdata="
	index := strings.Index(reportURL, marker)
	if index < 0 {
		t.Fatalf("report URL has no rdata: %s", reportURL)
	}
	encoded := reportURL[index+len(marker):]
	decoded, err := urlQueryUnescape(encoded)
	if err != nil {
		t.Fatalf("decode rdata: %v", err)
	}
	var data map[string]any
	decoder := json.NewDecoder(strings.NewReader(decoded))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
		t.Fatalf("unmarshal rdata: %v", err)
	}
	return data
}

func urlQueryUnescape(value string) (string, error) {
	// QueryUnescape is kept behind this helper so report tests cannot accidentally
	// use a query map that obscures ordering in assertions.
	return url.QueryUnescape(value)
}

func assertJSONNumber(t *testing.T, data map[string]any, key, want string) {
	t.Helper()
	got, present := data[key]
	if want == "" {
		if present {
			t.Fatalf("%s unexpectedly present: %#v", key, got)
		}
		return
	}
	number, ok := got.(json.Number)
	if !ok || number.String() != want {
		t.Fatalf("%s = %#v, want JSON number %s", key, got, want)
	}
}
