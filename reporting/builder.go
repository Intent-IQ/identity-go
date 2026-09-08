package reporting

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"
)

const (
	defaultCurrency        = "USD"
	biddingPlatformOpenRTB = "4"
	reportSource           = "pbsgo"
)

// buildReportURL builds the exact URL consumed by the reporting API.
func buildReportURL(request Request) string {
	currency := request.Currency
	if currency == "" {
		currency = defaultCurrency
	}

	rdata := newOrderedMap()
	rdata.put("bidderCode", request.BidderCode)
	rdata.put("partnerId", request.PartnerID)
	rdata.put("cpm", request.Bid.Price)
	rdata.put("currency", currency)
	appendOriginalBid(rdata, request.Bid.Ext)
	rdata.put("placementId", request.Bid.ImpID)
	rdata.put("biddingPlatformId", biddingPlatformOpenRTB)
	putIfPresent(rdata, "vrref", request.Reference)
	putIfPresent(rdata, "prebidAuctionId", request.AuctionID)
	putIfPresent(rdata, "partnerAuctionId", request.AuctionID)
	putIfPresent(rdata, "abTestUuid", request.ABTestUUID)
	if request.TerminationCause != nil {
		rdata.put("terminationCause", *request.TerminationCause)
	}
	putIfPresent(rdata, "ip", request.IP)
	putIfPresent(rdata, "ua", request.UserAgent)

	return assembleReportURL(request.Endpoint, request.PartnerID, rdata)
}

func assembleReportURL(endpoint, partnerID string, rdata *orderedMap) string {
	separator := "?"
	if strings.Contains(endpoint, "?") {
		separator = "&"
	}
	rdataJSON, _ := rdata.MarshalJSON()

	var builder strings.Builder
	builder.WriteString(endpoint)
	builder.WriteString(separator)
	builder.WriteString("at=45")
	builder.WriteString("&rtype=1")
	builder.WriteString("&source=" + reportSource)
	builder.WriteString("&dpi=" + encodeComponent(partnerID))
	builder.WriteString("&rdata=" + encodeComponent(string(rdataJSON)))
	return builder.String()
}

func appendOriginalBid(rdata *orderedMap, ext json.RawMessage) {
	if len(ext) == 0 {
		return
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(ext, &fields); err != nil {
		return
	}
	if raw, ok := fields["origbidcpm"]; ok {
		var number json.Number
		if err := json.Unmarshal(raw, &number); err == nil && number != "" {
			rdata.put("originalCpm", number)
		}
	}
	if raw, ok := fields["origbidcur"]; ok {
		var currency string
		if err := json.Unmarshal(raw, &currency); err == nil && notBlank(currency) {
			rdata.put("originalCurrency", currency)
		}
	}
}

func putIfPresent(rdata *orderedMap, key, value string) {
	if notBlank(value) {
		rdata.put(key, value)
	}
}

func encodeComponent(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func notBlank(value string) bool {
	return strings.TrimSpace(value) != ""
}

type orderedMap struct {
	keys   []string
	values map[string]any
}

func newOrderedMap() *orderedMap {
	return &orderedMap{values: make(map[string]any)}
}

func (m *orderedMap) put(key string, value any) {
	if _, exists := m.values[key]; !exists {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

func (m *orderedMap) MarshalJSON() ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for index, key := range m.keys {
		if index > 0 {
			buffer.WriteByte(',')
		}
		encodedKey, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		buffer.Write(encodedKey)
		buffer.WriteByte(':')
		encodedValue, err := json.Marshal(m.values[key])
		if err != nil {
			return nil, err
		}
		buffer.Write(encodedValue)
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}
