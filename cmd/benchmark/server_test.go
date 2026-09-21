package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Intent-IQ/identity-go/enrichment"
)

type requestCapturingEnricher struct {
	request enrichment.Request
}

func (stub *requestCapturingEnricher) Enrich(_ context.Context, request enrichment.Request) (enrichment.Result, error) {
	stub.request = request
	return enrichment.Result{Outcome: enrichment.OutcomeNoIDs}, nil
}

func TestBenchmarkHandlerSelectsWaitMode(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		wantWait *time.Duration
	}{
		{name: "sync", query: "mode=sync"},
		{name: "async", query: "mode=async", wantWait: durationPointer(0)},
		{name: "hybrid", query: "mode=hybrid&wait_ms=280", wantWait: durationPointer(280 * time.Millisecond)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := &requestCapturingEnricher{}
			handler := benchmarkHandler(serverConfig{timeout: 400 * time.Millisecond, cacheDir: "enabled"}, stub)
			request := httptest.NewRequest(http.MethodPost, "/enrich?"+test.query, strings.NewReader(`{"id":"auction"}`))
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if test.wantWait == nil && stub.request.WaitTimeout != nil {
				t.Fatalf("WaitTimeout = %v, want nil", stub.request.WaitTimeout)
			}
			if test.wantWait != nil && (stub.request.WaitTimeout == nil || *stub.request.WaitTimeout != *test.wantWait) {
				t.Fatalf("WaitTimeout = %v, want %v", stub.request.WaitTimeout, *test.wantWait)
			}
			var result record
			if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			if result.Outcome != string(enrichment.OutcomeNoIDs) {
				t.Fatalf("outcome = %q", result.Outcome)
			}
		})
	}
}

func TestBenchmarkHandlerRejectsInvalidModeConfiguration(t *testing.T) {
	tests := []string{
		"mode=unsupported",
		"mode=hybrid",
		"mode=hybrid&wait_ms=400",
		"mode=async",
	}
	for _, query := range tests {
		t.Run(query, func(t *testing.T) {
			handler := benchmarkHandler(serverConfig{timeout: 400 * time.Millisecond}, &requestCapturingEnricher{})
			request := httptest.NewRequest(http.MethodPost, "/enrich?"+query, strings.NewReader(`{}`))
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", response.Code, response.Body.String())
			}
		})
	}
}

func durationPointer(value time.Duration) *time.Duration { return &value }
