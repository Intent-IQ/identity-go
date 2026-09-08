package identitymodule

import (
	"strings"

	"github.com/prebid/openrtb/v20/openrtb2"
	"github.com/prebid/prebid-server/v4/hooks/hookstage"
)

const auctionContextKey = "intentiq.identity.auction"

// auctionContext carries request and enrichment data to the response hook.
type auctionContext struct {
	AuctionID        string
	Reference        string
	IP               string
	UserAgent        string
	ABTestUUID       string
	TerminationCause *int64
}

func newAuctionContext(auction *openrtb2.BidRequest) auctionContext {
	if auction == nil {
		return auctionContext{}
	}

	result := auctionContext{AuctionID: auction.ID}

	if auction.Site != nil {
		result.Reference = auction.Site.Domain
		if !notBlank(result.Reference) {
			result.Reference = auction.Site.Page
		}
	} else if auction.App != nil {
		result.Reference = auction.App.Bundle
		if !notBlank(result.Reference) {
			result.Reference = auction.App.Name
		}
	}

	if auction.Device != nil {
		result.IP = auction.Device.IP
		if result.IP == "" {
			result.IP = auction.Device.IPv6
		}
		result.UserAgent = auction.Device.UA
	}

	return result
}

func storeAuctionContext(value auctionContext) *hookstage.ModuleContext {
	moduleContext := hookstage.NewModuleContext()
	moduleContext.Set(auctionContextKey, value)
	return moduleContext
}

func loadAuctionContext(moduleContext *hookstage.ModuleContext) (auctionContext, bool) {
	if moduleContext == nil {
		return auctionContext{}, false
	}
	value, found := moduleContext.Get(auctionContextKey)
	if !found {
		return auctionContext{}, false
	}
	result, valid := value.(auctionContext)
	return result, valid
}

func notBlank(value string) bool {
	return strings.TrimSpace(value) != ""
}
