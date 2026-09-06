package identitymodule

import (
	"context"
	"errors"
	"testing"

	iiqidenrichment "github.com/Intent-IQ/identity-go/enrichment"
	"github.com/prebid/openrtb/v20/openrtb2"
	"github.com/prebid/prebid-server/v4/hooks/hookstage"
	"github.com/prebid/prebid-server/v4/openrtb_ext"
)

func TestProcessedAuctionRequestEnrichesAndStoresAuctionContext(t *testing.T) {
	tc := int64(7)
	module := &Module{
		config: Config{PartnerID: "partner", APIEndpoint: "endpoint", Timeout: 250},
		enricher: enricherStub{result: iiqidenrichment.Result{
			EIDs:             []openrtb2.EID{{Source: "intentiq.com"}},
			ABTestUUID:       "ab-test",
			TerminationCause: &tc,
		}},
	}
	auction := &openrtb2.BidRequest{
		ID:     "auction",
		Site:   &openrtb2.Site{Domain: "publisher.example"},
		Device: &openrtb2.Device{IP: "192.0.2.1", UA: "ua"},
	}
	payload := hookstage.ProcessedAuctionRequestPayload{
		Request: &openrtb_ext.RequestWrapper{BidRequest: auction},
	}

	result, err := module.HandleProcessedAuctionHook(t.Context(), hookstage.ModuleInvocationContext{}, payload)
	if err != nil || result.Reject {
		t.Fatalf("hook returned err=%v reject=%v", err, result.Reject)
	}
	mutations := result.ChangeSet.Mutations()
	if len(mutations) != 1 {
		t.Fatalf("mutations = %d, want 1", len(mutations))
	}
	if _, err := mutations[0].Apply(payload); err != nil {
		t.Fatalf("apply mutation: %v", err)
	}
	if len(auction.User.EIDs) != 1 || auction.User.EIDs[0].Source != "intentiq.com" {
		t.Fatalf("auction eids = %#v", auction.User.EIDs)
	}
	stored, ok := loadAuctionContext(result.ModuleContext)
	if !ok || stored.AuctionID != "auction" || stored.Reference != "publisher.example" || stored.IP != "192.0.2.1" || stored.UserAgent != "ua" || stored.ABTestUUID != "ab-test" || stored.TerminationCause != &tc {
		t.Fatalf("auction context = %#v, found=%v", stored, ok)
	}
}

func TestProcessedAuctionRequestFailsOpen(t *testing.T) {
	module := &Module{config: Config{APIEndpoint: "endpoint"}, enricher: enricherStub{err: errors.New("unavailable")}}
	auction := &openrtb2.BidRequest{User: &openrtb2.User{}}
	payload := hookstage.ProcessedAuctionRequestPayload{Request: &openrtb_ext.RequestWrapper{BidRequest: auction}}

	result, err := module.HandleProcessedAuctionHook(t.Context(), hookstage.ModuleInvocationContext{}, payload)
	if err != nil || result.Reject || len(result.ChangeSet.Mutations()) != 0 || len(auction.User.EIDs) != 0 {
		t.Fatalf("fail-open result=%#v err=%v eids=%#v", result, err, auction.User.EIDs)
	}
}

type enricherStub struct {
	result iiqidenrichment.Result
	err    error
}

func (stub enricherStub) Enrich(context.Context, iiqidenrichment.Request) (iiqidenrichment.Result, error) {
	return stub.result, stub.err
}

var _ iiqidenrichment.Enricher = enricherStub{}
