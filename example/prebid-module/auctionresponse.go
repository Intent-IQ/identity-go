package identitymodule

import (
	"context"
	"log/slog"
	"runtime/debug"

	iiqidreporting "github.com/Intent-IQ/identity-go/reporting"
	"github.com/prebid/prebid-server/v4/hooks/hookstage"
)

// HandleAuctionResponseHook queues one report per bid without changing the response.
func (module *Module) HandleAuctionResponseHook(
	_ context.Context,
	invocation hookstage.ModuleInvocationContext,
	payload hookstage.AuctionResponsePayload,
) (result hookstage.HookResult[hookstage.AuctionResponsePayload], _ error) {
	config := module.config.resolve(invocation.AccountConfig)
	if config.ReportsEndpoint == "" || payload.BidResponse == nil {
		return result, nil
	}

	auction, _ := loadAuctionContext(invocation.ModuleContext)
	for seatIndex := range payload.BidResponse.SeatBid {
		seatBid := payload.BidResponse.SeatBid[seatIndex]
		for bidIndex := range seatBid.Bid {
			request := iiqidreporting.Request{
				PartnerID:        config.PartnerID,
				Endpoint:         config.ReportsEndpoint,
				Timeout:          config.timeout(),
				Bid:              seatBid.Bid[bidIndex],
				BidderCode:       seatBid.Seat,
				Currency:         payload.BidResponse.Cur,
				AuctionID:        auction.AuctionID,
				Reference:        auction.Reference,
				IP:               auction.IP,
				UserAgent:        auction.UserAgent,
				ABTestUUID:       auction.ABTestUUID,
				TerminationCause: auction.TerminationCause,
			}

			go module.report(request)
		}
	}
	return result, nil
}

func (module *Module) report(request iiqidreporting.Request) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("panic in impression report", "panic", recovered, "stack", string(debug.Stack()))
		}
	}()

	_ = module.reporter.Report(context.Background(), request)
}
