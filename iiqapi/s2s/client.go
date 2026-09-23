package s2s

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	iiqapi "github.com/Intent-IQ/identity-go/iiqapi"
	"github.com/prebid/openrtb/v20/openrtb2"
)

const maxErrorSnippetSize = 1024

// Client implements API over HTTP.
type Client struct{ httpClient *http.Client }

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{httpClient: httpClient}
}

func (c *Client) Resolve(ctx context.Context, requestURL, consent string) (Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return Response{}, &iiqapi.Error{Kind: iiqapi.ErrorRequest, Err: err}
	}
	if consent != "" {
		req.Header.Set(GDPRConsentHeader, consent)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		kind := iiqapi.ErrorTransport
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			kind = iiqapi.ErrorTimeout
		}
		return Response{}, &iiqapi.Error{Kind: kind, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorSnippetSize))
		_, _ = io.Copy(io.Discard, resp.Body)
		return Response{}, &iiqapi.Error{
			Kind:            iiqapi.ErrorStatus,
			Status:          resp.StatusCode,
			ResponseSnippet: strings.Join(strings.Fields(string(body)), " "),
			Err:             fmt.Errorf("resolution API returned %d", resp.StatusCode),
		}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, &iiqapi.Error{Kind: iiqapi.ErrorBodyRead, Status: resp.StatusCode, Err: err}
	}
	// The S2S client may return 200 with an empty body.
	// Treat it as a valid response with no IDs instead of failing to parse it.
	if len(bytes.TrimSpace(body)) == 0 {
		return Response{Status: resp.StatusCode, EmptyBody: true}, nil
	}
	result, err := decodeResponse(body)
	if err != nil {
		return Response{}, &iiqapi.Error{Kind: iiqapi.ErrorParse, Status: resp.StatusCode, Err: err}
	}
	result.Status = resp.StatusCode
	return result, nil
}

func decodeResponse(body []byte) (Response, error) {
	var wire struct {
		Data struct {
			EIDs []openrtb2.EID `json:"eids"`
		} `json:"data"`
		CacheTTL   *int64 `json:"cttl"`
		ABTestUUID string `json:"abTestUuid"`
		TC         *int64 `json:"tc"`
	}
	if err := json.Unmarshal(body, &wire); err == nil {
		return Response{
			ResolvedEIDs: wire.Data.EIDs,
			CacheTTL:     wire.CacheTTL,
			ABTestUUID:   wire.ABTestUUID,
			TC:           wire.TC,
		}, nil
	}

	// Preserve the API's historically lenient handling of non-object data.
	var result Response
	if err := json.Unmarshal(body, &result); err != nil {
		return Response{}, err
	}
	result.ResolvedEIDs = result.EIDs()
	return result, nil
}

var _ API = (*Client)(nil)
