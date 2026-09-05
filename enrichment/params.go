package enrichment

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/prebid/openrtb/v20/openrtb2"
)

const (
	paramAT        = "at"
	paramMI        = "mi"
	paramDPI       = "dpi"
	paramPT        = "pt"
	paramDPN       = "dpn"
	paramServerReq = "srvrReq"
	paramSource    = "source"
	paramIP        = "ip"
	paramIPv6      = "ipv6"
	paramUA        = "uas"
	paramUAHints   = "uh"
	paramPCID      = "pcid"
	paramIDType    = "idtype"
	paramRef       = "ref"
	paramIIQUID    = "iiquid"
	paramGDPR      = "gdpr"
	paramUSPrivacy = "us_privacy"
	paramGPP       = "gpp"
	paramGPPSID    = "gpp_sid"

	iiqSource = "intentiq.com"
	apiSource = "pbsgo"
)

// buildS2SRequest builds the URL and consent header value expected by the S2S API.
func buildS2SRequest(input Request) (requestURL, consent string) {
	var builder strings.Builder
	builder.WriteString(input.Endpoint)
	if strings.Contains(input.Endpoint, "?") {
		builder.WriteByte('&')
	} else {
		builder.WriteByte('?')
	}
	builder.WriteString(paramAT)
	builder.WriteString("=39")

	appendParameter(&builder, paramMI, "10")
	appendParameter(&builder, paramDPI, input.PartnerID)
	appendParameter(&builder, paramPT, "17")
	appendParameter(&builder, paramDPN, "1")
	appendParameter(&builder, paramServerReq, "true")
	appendParameter(&builder, paramSource, apiSource)

	request := input.Auction
	if request == nil {
		return builder.String(), ""
	}

	if device := request.Device; device != nil {
		appendParameter(&builder, paramIP, device.IP)
		appendParameter(&builder, paramIPv6, device.IPv6)
		appendParameter(&builder, paramUA, device.UA)
		appendParameter(&builder, paramUAHints, buildUAHints(device.SUA))
		appendDeviceID(&builder, device)
	}
	appendParameter(&builder, paramRef, resolveRef(request))
	appendParameter(&builder, paramIIQUID, resolveIIQUID(request.User))
	appendPrivacyParameters(&builder, request.Regs)

	return builder.String(), resolveConsent(request.User)
}

func appendDeviceID(builder *strings.Builder, device *openrtb2.Device) {
	if !notBlank(device.IFA) || (device.Lmt != nil && *device.Lmt == 1) {
		return
	}

	pcid := device.IFA
	idType := "4"
	if device.DeviceType == 3 || device.DeviceType == 7 {
		pcid = strings.ToUpper(pcid)
		idType = "8"
	}
	appendParameter(builder, paramPCID, pcid)
	appendParameter(builder, paramIDType, idType)
}

func resolveRef(request *openrtb2.BidRequest) string {
	if site := request.Site; site != nil {
		if notBlank(site.Domain) {
			return site.Domain
		}
		return site.Page
	}
	if app := request.App; app != nil {
		if notBlank(app.Bundle) {
			return app.Bundle
		}
		return app.Name
	}
	return ""
}

func resolveIIQUID(user *openrtb2.User) string {
	if user == nil {
		return ""
	}
	for _, eid := range user.EIDs {
		if eid.Source != iiqSource {
			continue
		}
		for _, uid := range eid.UIDs {
			if notBlank(uid.ID) {
				return uid.ID
			}
		}
	}
	return ""
}

type regsExtension struct {
	GDPR      *int8  `json:"gdpr"`
	USPrivacy string `json:"us_privacy"`
}

func appendPrivacyParameters(builder *strings.Builder, regs *openrtb2.Regs) {
	if regs == nil {
		return
	}

	var extension regsExtension
	extensionValid := len(regs.Ext) > 0 && json.Unmarshal(regs.Ext, &extension) == nil

	if regs.GDPR != nil {
		appendParameter(builder, paramGDPR, strconv.Itoa(int(*regs.GDPR)))
	} else if extensionValid && extension.GDPR != nil {
		appendParameter(builder, paramGDPR, strconv.Itoa(int(*extension.GDPR)))
	}

	if notBlank(regs.USPrivacy) {
		appendParameter(builder, paramUSPrivacy, regs.USPrivacy)
	} else if extensionValid {
		appendParameter(builder, paramUSPrivacy, extension.USPrivacy)
	}
	appendParameter(builder, paramGPP, regs.GPP)
	appendParameter(builder, paramGPPSID, joinGPPSID(regs.GPPSID))
}

type userExtension struct {
	Consent *string `json:"consent"`
}

func resolveConsent(user *openrtb2.User) string {
	if user == nil {
		return ""
	}
	if notBlank(user.Consent) {
		return user.Consent
	}

	var extension userExtension
	if len(user.Ext) == 0 || json.Unmarshal(user.Ext, &extension) != nil || extension.Consent == nil {
		return ""
	}
	return *extension.Consent
}

func joinGPPSID(sectionIDs []int8) string {
	if len(sectionIDs) == 0 {
		return ""
	}
	parts := make([]string, len(sectionIDs))
	for index, sectionID := range sectionIDs {
		parts[index] = strconv.Itoa(int(sectionID))
	}
	return strings.Join(parts, ",")
}

func appendParameter(builder *strings.Builder, name, value string) {
	if !notBlank(value) {
		return
	}
	builder.WriteByte('&')
	builder.WriteString(name)
	builder.WriteByte('=')
	builder.WriteString(encodeParameter(value))
}

func encodeParameter(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func notBlank(value string) bool {
	return strings.TrimSpace(value) != ""
}
