package reporting

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Intent-IQ/identity-go/iiqapi"
)

func TestReportImpressionSendsExactURLWithGET(t *testing.T) {
	request := make(chan struct {
		method string
		uri    string
	}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request <- struct {
			method string
			uri    string
		}{method: r.Method, uri: r.RequestURI}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	requestURL := server.URL + "/report?z=hello%20world&a=1&a=2&rdata=%7B%22x%22%3A1%7D"
	if err := NewClient(server.Client()).ReportImpression(t.Context(), requestURL); err != nil {
		t.Fatalf("ReportImpression() error = %v", err)
	}
	got := <-request
	if got.method != http.MethodGet {
		t.Fatalf("method = %q, want GET", got.method)
	}
	if want := "/report?z=hello%20world&a=1&a=2&rdata=%7B%22x%22%3A1%7D"; got.uri != want {
		t.Fatalf("RequestURI = %q, want %q", got.uri, want)
	}
}

func TestReportImpressionTreatsEveryResponseStatusAsSuccess(t *testing.T) {
	statuses := []int{
		http.StatusOK,
		http.StatusNoContent,
		http.StatusFound,
		http.StatusBadRequest,
		http.StatusInternalServerError,
	}
	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "response body")
			}))
			t.Cleanup(server.Close)

			httpClient := server.Client()
			httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			}
			if err := NewClient(httpClient).ReportImpression(t.Context(), server.URL); err != nil {
				t.Fatalf("ReportImpression() for status %d error = %v", status, err)
			}
		})
	}
}

func TestReportImpressionRequestError(t *testing.T) {
	err := NewClient(http.DefaultClient).ReportImpression(t.Context(), "://invalid")
	assertAPIError(t, err, iiqapi.ErrorRequest)
}

func TestReportImpressionTransportError(t *testing.T) {
	cause := errors.New("transport failed")
	httpClient := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, cause
	})}

	err := NewClient(httpClient).ReportImpression(t.Context(), "https://example.test/report")
	assertAPIError(t, err, iiqapi.ErrorTransport)
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want cause in error chain", err)
	}
}

func TestReportImpressionDeadlineIsTimeout(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()

	err := NewClient(httpClient).ReportImpression(ctx, "https://example.test/report")
	assertAPIError(t, err, iiqapi.ErrorTimeout)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded in error chain", err)
	}
}

func TestReportImpressionDrainsAndClosesBody(t *testing.T) {
	body := &recordingBody{reader: strings.NewReader("complete response")}
	httpClient := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusTeapot, Body: body, Header: make(http.Header)}, nil
	})}

	if err := NewClient(httpClient).ReportImpression(t.Context(), "https://example.test/report"); err != nil {
		t.Fatalf("ReportImpression() error = %v", err)
	}
	if !body.closed {
		t.Fatal("response body was not closed")
	}
	if got, want := body.read.String(), "complete response"; got != want {
		t.Fatalf("drained body = %q, want %q", got, want)
	}
}

func TestReportImpressionIgnoresBodyErrors(t *testing.T) {
	body := &errorBody{readErr: errors.New("read failed"), closeErr: errors.New("close failed")}
	httpClient := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
	})}

	if err := NewClient(httpClient).ReportImpression(t.Context(), "https://example.test/report"); err != nil {
		t.Fatalf("ReportImpression() error = %v, want nil", err)
	}
	if !body.closed {
		t.Fatal("response body was not closed after read failure")
	}
}

func TestReportImpressionReusesConnection(t *testing.T) {
	peers := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peers <- r.RemoteAddr
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, strings.Repeat("x", 4096))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.Client())
	for range 2 {
		if err := client.ReportImpression(t.Context(), server.URL); err != nil {
			t.Fatalf("ReportImpression() error = %v", err)
		}
	}
	first, second := <-peers, <-peers
	if first != second {
		t.Fatalf("remote addresses = %q and %q, want connection reuse", first, second)
	}
}

func TestReportImpressionConcurrentCalls(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.Client())
	const callCount = 32
	var waitGroup sync.WaitGroup
	errorsFound := make(chan error, callCount)
	for range callCount {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if err := client.ReportImpression(t.Context(), server.URL); err != nil {
				errorsFound <- err
			}
		}()
	}
	waitGroup.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("ReportImpression() error = %v", err)
	}
	if got := calls.Load(); got != callCount {
		t.Fatalf("calls = %d, want %d", got, callCount)
	}
}

func assertAPIError(t *testing.T, err error, wantKind iiqapi.ErrorKind) {
	t.Helper()
	var apiError *iiqapi.Error
	if !errors.As(err, &apiError) {
		t.Fatalf("error = %v, want *iiqapi.Error", err)
	}
	if apiError.Kind != wantKind {
		t.Fatalf("error kind = %q, want %q", apiError.Kind, wantKind)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type recordingBody struct {
	reader io.Reader
	read   strings.Builder
	closed bool
}

func (body *recordingBody) Read(buffer []byte) (int, error) {
	count, err := body.reader.Read(buffer)
	body.read.Write(buffer[:count])
	return count, err
}

func (body *recordingBody) Close() error {
	body.closed = true
	return nil
}

type errorBody struct {
	readErr  error
	closeErr error
	closed   bool
}

func (body *errorBody) Read([]byte) (int, error) { return 0, body.readErr }
func (body *errorBody) Close() error {
	body.closed = true
	return body.closeErr
}
