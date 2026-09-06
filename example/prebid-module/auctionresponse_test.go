package identitymodule

import (
	"context"
	"reflect"
	"testing"
	"time"

	iiqidreporting "github.com/Intent-IQ/identity-go/reporting"
	"github.com/prebid/openrtb/v20/openrtb2"
	"github.com/prebid/prebid-server/v4/hooks/hookstage"
)

func TestAuctionResponseReportsEveryBidWithoutMutation(t *testing.T) {
	reports := make(chan iiqidreporting.Request, 2)
	module := &Module{
		config:   Config{PartnerID: "partner", ReportsEndpoint: "reports", Timeout: 250},
		reporter: reporterStub{reports: reports},
	}
	tc := int64(0)
	invocation := hookstage.ModuleInvocationContext{ModuleContext: storeAuctionContext(auctionContext{
		AuctionID: "auction", Reference: "publisher.example", IP: "192.0.2.1",
		UserAgent: "ua", ABTestUUID: "ab-test", TerminationCause: &tc,
	})}
	response := &openrtb2.BidResponse{Cur: "EUR", SeatBid: []openrtb2.SeatBid{{
		Seat: "bidder", Bid: []openrtb2.Bid{{ImpID: "imp-1", Price: 1}, {ImpID: "imp-2", Price: 2}},
	}}}
	wantResponse := cloneBidResponse(t, response)

	result, err := module.HandleAuctionResponseHook(t.Context(), invocation, hookstage.AuctionResponsePayload{BidResponse: response})
	if err != nil || result.Reject || len(result.ChangeSet.Mutations()) != 0 {
		t.Fatalf("hook returned result=%#v err=%v", result, err)
	}
	for index := 1; index <= 2; index++ {
		select {
		case report := <-reports:
			if report.PartnerID != "partner" || report.BidderCode != "bidder" || report.Currency != "EUR" || report.AuctionID != "auction" || report.ABTestUUID != "ab-test" || report.TerminationCause != &tc {
				t.Fatalf("report = %#v", report)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for report %d", index)
		}
	}
	if !reflect.DeepEqual(response, wantResponse) {
		t.Fatalf("bid response mutated: got %#v want %#v", response, wantResponse)
	}
}

func cloneBidResponse(t *testing.T, response *openrtb2.BidResponse) *openrtb2.BidResponse {
	t.Helper()
	clone := *response
	clone.SeatBid = append([]openrtb2.SeatBid(nil), response.SeatBid...)
	for index := range clone.SeatBid {
		clone.SeatBid[index].Bid = append([]openrtb2.Bid(nil), response.SeatBid[index].Bid...)
	}
	return &clone
}

type reporterStub struct {
	reports chan<- iiqidreporting.Request
}

func (stub reporterStub) Report(_ context.Context, request iiqidreporting.Request) error {
	stub.reports <- request
	return nil
}

var _ iiqidreporting.Reporter = reporterStub{}
