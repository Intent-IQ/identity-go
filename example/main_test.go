package main

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Intent-IQ/identity-go/enrichment"
	identityreporting "github.com/Intent-IQ/identity-go/reporting"
)

func TestReportBidResponseMapsFlowWithoutMutation(t *testing.T) {
	recorder := &recordingReporter{}
	cfg := config{
		PartnerID:       "partner",
		TimeoutMS:       250,
		ReportsEndpoint: "https://reports.example/path",
	}
	auction := exampleAuction()
	terminationCause := int64(0)
	result := enrichment.Result{ABTestUUID: "ab-1", TerminationCause: &terminationCause}
	bidResponse := exampleBidResponse()
	wantBidResponse := exampleBidResponse()

	pending := reportBidResponse(recorder, cfg, auction, result, bidResponse)
	pending.Wait()

	if pending.count != 1 {
		t.Fatalf("report count = %d, want 1", pending.count)
	}
	requests := recorder.snapshot()
	if len(requests) != 1 {
		t.Fatalf("recorded requests = %d, want 1", len(requests))
	}
	request := requests[0]
	if request.PartnerID != cfg.PartnerID || request.Endpoint != cfg.ReportsEndpoint || request.Timeout != 250*time.Millisecond {
		t.Fatalf("configuration mapping = %#v", request)
	}
	if request.BidderCode != "example-bidder" || request.Currency != "EUR" || request.Bid.ImpID != "example-imp" {
		t.Fatalf("bid mapping = %#v", request)
	}
	if request.AuctionID != auction.ID || request.Reference != "example.com" || request.IP != "192.0.2.1" || request.UserAgent != "identity-go-example" {
		t.Fatalf("auction mapping = %#v", request)
	}
	if request.ABTestUUID != "ab-1" || request.TerminationCause == nil || *request.TerminationCause != 0 {
		t.Fatalf("enrichment result mapping = %#v", request)
	}
	if !reflect.DeepEqual(bidResponse, wantBidResponse) {
		t.Fatalf("bid response was mutated:\n got: %#v\nwant: %#v", bidResponse, wantBidResponse)
	}
}

func TestReportBidResponseSkipsMissingEndpointOrResponse(t *testing.T) {
	recorder := &recordingReporter{}
	auction := exampleAuction()

	withoutEndpoint := reportBidResponse(recorder, config{}, auction, enrichment.Result{}, exampleBidResponse())
	withoutEndpoint.Wait()
	withoutResponse := reportBidResponse(recorder, config{ReportsEndpoint: "endpoint"}, auction, enrichment.Result{}, nil)
	withoutResponse.Wait()

	if withoutEndpoint.count != 0 || withoutResponse.count != 0 || len(recorder.snapshot()) != 0 {
		t.Fatal("reporting was not skipped")
	}
}

type recordingReporter struct {
	mutex    sync.Mutex
	requests []identityreporting.Request
}

func (reporter *recordingReporter) Report(_ context.Context, request identityreporting.Request) error {
	reporter.mutex.Lock()
	defer reporter.mutex.Unlock()
	reporter.requests = append(reporter.requests, request)
	return nil
}

func (reporter *recordingReporter) snapshot() []identityreporting.Request {
	reporter.mutex.Lock()
	defer reporter.mutex.Unlock()
	return append([]identityreporting.Request(nil), reporter.requests...)
}

var _ identityreporting.Reporter = (*recordingReporter)(nil)
