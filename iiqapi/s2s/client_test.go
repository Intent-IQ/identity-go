package s2s

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Intent-IQ/identity-go/iiqapi"
)

func TestResolveParsesResponse(t *testing.T) {
	server := responseServer(t, http.StatusOK,
		`{"data":{"eids":[{"source":"intentiq.com","uids":[{"id":"x"}]}]},`+
			`"cttl":60,"abTestUuid":"ab-1","tc":120088}`,
	)

	got, err := NewClient(server.Client()).Resolve(t.Context(), server.URL, "")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Status != http.StatusOK {
		t.Fatalf("Status = %d, want %d", got.Status, http.StatusOK)
	}
	if got.ABTestUUID != "ab-1" {
		t.Fatalf("ABTestUUID = %q, want %q", got.ABTestUUID, "ab-1")
	}
	if got.TC == nil || *got.TC != 120088 {
		t.Fatalf("TC = %v, want 120088", got.TC)
	}
	if got.TTL() != 60*time.Second {
		t.Fatalf("TTL() = %v, want %v", got.TTL(), 60*time.Second)
	}
	eids := got.EIDs()
	if len(eids) != 1 || eids[0].Source != "intentiq.com" || len(eids[0].UIDs) != 1 || eids[0].UIDs[0].ID != "x" {
		t.Fatalf("EIDs() = %#v, want one intentiq.com EID with UID x", eids)
	}
}

func TestResolveEmptyDataIsSuccess(t *testing.T) {
	server := responseServer(t, http.StatusOK, `{"data":"","cttl":30}`)

	got, err := NewClient(server.Client()).Resolve(t.Context(), server.URL, "")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.EIDs() != nil {
		t.Fatalf("EIDs() = %#v, want nil", got.EIDs())
	}
	if got.TTL() != 30*time.Second {
		t.Fatalf("TTL() = %v, want %v", got.TTL(), 30*time.Second)
	}
}

func TestResolveSendsOptionalConsentHeader(t *testing.T) {
	consents := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		consents <- r.Header.Get(GDPRConsentHeader)
		_, _ = io.WriteString(w, `{"data":""}`)
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.Client())
	if _, err := client.Resolve(t.Context(), server.URL, "CONSENT-STRING"); err != nil {
		t.Fatalf("Resolve() with consent error = %v", err)
	}
	if got := <-consents; got != "CONSENT-STRING" {
		t.Fatalf("consent header = %q, want %q", got, "CONSENT-STRING")
	}
	if _, err := client.Resolve(t.Context(), server.URL, ""); err != nil {
		t.Fatalf("Resolve() without consent error = %v", err)
	}
	if got := <-consents; got != "" {
		t.Fatalf("consent header = %q, want empty", got)
	}
}

func TestResolvePreservesSuppliedURL(t *testing.T) {
	requestURI := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestURI <- r.RequestURI
		_, _ = io.WriteString(w, `{"data":""}`)
	}))
	t.Cleanup(server.Close)

	requestURL := server.URL + "/resolve?z=hello%20world&a=1&a=2"
	if _, err := NewClient(server.Client()).Resolve(t.Context(), requestURL, ""); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got, want := <-requestURI, "/resolve?z=hello%20world&a=1&a=2"; got != want {
		t.Fatalf("RequestURI = %q, want %q", got, want)
	}
}

func TestResolveRequestError(t *testing.T) {
	_, err := NewClient(http.DefaultClient).Resolve(t.Context(), "://invalid", "")
	assertAPIError(t, err, iiqapi.ErrorRequest, 0)
}

func TestResolveNon2xx(t *testing.T) {
	server := responseServer(t, http.StatusForbidden, "invalid\n partner\t token")

	_, err := NewClient(server.Client()).Resolve(t.Context(), server.URL, "")
	apiErr := assertAPIError(t, err, iiqapi.ErrorStatus, http.StatusForbidden)
	if apiErr.ResponseSnippet != "invalid partner token" {
		t.Fatalf("ResponseSnippet = %q, want %q", apiErr.ResponseSnippet, "invalid partner token")
	}
	kind, status := iiqapi.ErrorLabels(err)
	if kind != "status" || status != "403" {
		t.Fatalf("ErrorLabels() = (%q, %q), want (%q, %q)", kind, status, "status", "403")
	}
}

func TestResolveSnippetIsCapped(t *testing.T) {
	server := responseServer(t, http.StatusInternalServerError, strings.Repeat("x", 10_000))

	_, err := NewClient(server.Client()).Resolve(t.Context(), server.URL, "")
	apiErr := assertAPIError(t, err, iiqapi.ErrorStatus, http.StatusInternalServerError)
	if len(apiErr.ResponseSnippet) != maxErrorSnippetSize {
		t.Fatalf("snippet length = %d, want %d", len(apiErr.ResponseSnippet), maxErrorSnippetSize)
	}
}

func TestResolveTransportError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := server.Client()
	requestURL := server.URL
	server.Close()

	_, err := NewClient(client).Resolve(t.Context(), requestURL, "")
	assertAPIError(t, err, iiqapi.ErrorTransport, 0)
}

func TestResolveDeadlineIsTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err := NewClient(server.Client()).Resolve(ctx, server.URL, "")
	assertAPIError(t, err, iiqapi.ErrorTimeout, 0)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded in chain", err)
	}
}

func TestResolveBodyReadError(t *testing.T) {
	body := &failingReadCloser{err: errors.New("read failed")}
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
	})}

	_, err := NewClient(client).Resolve(t.Context(), "https://example.test", "")
	assertAPIError(t, err, iiqapi.ErrorBodyRead, http.StatusOK)
	if !body.closed {
		t.Fatal("response body was not closed")
	}
}

func TestResolveParseError(t *testing.T) {
	server := responseServer(t, http.StatusOK, `{not-json}`)

	_, err := NewClient(server.Client()).Resolve(t.Context(), server.URL, "")
	assertAPIError(t, err, iiqapi.ErrorParse, http.StatusOK)
}

func TestResolveReusesConnectionAfterNon2xx(t *testing.T) {
	peers := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peers <- r.RemoteAddr
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, strings.Repeat("x", 2048))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.Client())
	for range 2 {
		_, err := client.Resolve(t.Context(), server.URL, "")
		if err == nil {
			t.Fatal("Resolve() error = nil, want non-2xx error")
		}
	}
	first, second := <-peers, <-peers
	if first != second {
		t.Fatalf("remote addresses = %q and %q, want connection reuse", first, second)
	}
}

func TestResponseHelpersAreLenient(t *testing.T) {
	if (Response{}).TTL() != 0 {
		t.Fatalf("empty response TTL = %v, want 0", (Response{}).TTL())
	}
	for _, data := range []string{``, `""`, ` `, `[]`, `{not-json}`} {
		if got := (Response{Data: []byte(data)}).EIDs(); got != nil {
			t.Fatalf("EIDs() for %q = %#v, want nil", data, got)
		}
	}
}

func responseServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

func assertAPIError(t *testing.T, err error, kind iiqapi.ErrorKind, status int) *iiqapi.Error {
	t.Helper()
	var apiErr *iiqapi.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *iiqapi.Error", err)
	}
	if apiErr.Kind != kind || apiErr.Status != status {
		t.Fatalf("API error = {Kind:%q Status:%d}, want {Kind:%q Status:%d}", apiErr.Kind, apiErr.Status, kind, status)
	}
	return apiErr
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type failingReadCloser struct {
	err    error
	closed bool
}

func (r *failingReadCloser) Read([]byte) (int, error) { return 0, r.err }
func (r *failingReadCloser) Close() error {
	r.closed = true
	return nil
}

func ExampleAPI() {
	var _ API = NewClient(http.DefaultClient)
	fmt.Println("s2s client implements API")
	// Output: s2s client implements API
}
