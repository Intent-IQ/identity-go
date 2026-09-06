package identitymodule

import (
	"context"

	iiqidenrichment "github.com/Intent-IQ/identity-go/enrichment"
	"github.com/prebid/openrtb/v20/openrtb2"
	"github.com/prebid/prebid-server/v4/hooks/hookstage"
)

// HandleProcessedAuctionHook enriches the request and remains fail-open.
func (module *Module) HandleProcessedAuctionHook(
	ctx context.Context,
	invocation hookstage.ModuleInvocationContext,
	payload hookstage.ProcessedAuctionRequestPayload,
) (result hookstage.HookResult[hookstage.ProcessedAuctionRequestPayload], _ error) {
	config := module.config.resolve(invocation.AccountConfig)
	var auction *openrtb2.BidRequest
	if payload.Request != nil {
		auction = payload.Request.BidRequest
	}

	contextValue := newAuctionContext(auction)
	defer func() { result.ModuleContext = storeAuctionContext(contextValue) }()

	resolved, err := module.enricher.Enrich(ctx, iiqidenrichment.Request{
		PartnerID:    config.PartnerID,
		Endpoint:     config.APIEndpoint,
		Auction:      auction,
		Timeout:      config.timeout(),
		CacheEnabled: config.Cache.Enabled,
	})
	if err != nil {
		// Prebid Server owns fail-open policy: an API failure does not reject or mutate the auction.
		return result, nil
	}

	contextValue.ABTestUUID = resolved.ABTestUUID
	contextValue.TerminationCause = resolved.TerminationCause

	if len(resolved.EIDs) == 0 {
		return result, nil
	}

	eids := append([]openrtb2.EID(nil), resolved.EIDs...)
	result.ChangeSet.AddMutation(func(value hookstage.ProcessedAuctionRequestPayload) (hookstage.ProcessedAuctionRequestPayload, error) {
		if value.Request == nil || value.Request.BidRequest == nil {
			return value, nil
		}
		if value.Request.BidRequest.User == nil {
			value.Request.BidRequest.User = &openrtb2.User{}
		}
		value.Request.BidRequest.User.EIDs = append(value.Request.BidRequest.User.EIDs, eids...)
		return value, nil
	}, hookstage.MutationUpdate, "bidrequest", "user", "eids")
	return result, nil
}
