// Package s2s implements the IIQ identity-resolution S2S API.
package s2s

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	iiqapi "github.com/Intent-IQ/identity-go/iiqapi"
	"github.com/prebid/openrtb/v20/openrtb2"
)

const maxErrorSnippetSize = 1024

// API resolves identities through the IIQ S2S API.
type API interface {
	ResolveIdentity(context.Context, Request) (Response, error)
}

type Request struct {
	Endpoint string
	Params   url.Values
	Consent  string
}

type Response struct {
	Data       json.RawMessage `json:"data"`
	CacheTTL   *int64          `json:"cttl"`
	ABTestUUID string          `json:"abTestUuid"`
	TC         *int64          `json:"tc"`
	Status     int             `json:"-"`
}

func (r Response) EIDs() []openrtb2.EID {
	var data struct {
		EIDs []openrtb2.EID `json:"eids"`
	}
	if err := json.Unmarshal(r.Data, &data); err != nil {
		return nil
	}
	return data.EIDs
}

func (r Response) TTL() time.Duration {
	if r.CacheTTL == nil {
		return 0
	}
	return time.Duration(*r.CacheTTL) * time.Second
}

type Client struct{ httpClient *http.Client }

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{httpClient: httpClient}
}

func (c *Client) ResolveIdentity(ctx context.Context, input Request) (Response, error) {
	requestURL, err := addQuery(input.Endpoint, input.Params)
	if err != nil {
		return Response{}, &iiqapi.Error{Kind: iiqapi.ErrorRequest, Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return Response{}, &iiqapi.Error{Kind: iiqapi.ErrorRequest, Err: err}
	}
	if input.Consent != "" {
		req.Header.Set("gdpr-consent", input.Consent)
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
			Err:             fmt.Errorf("S2S API returned %d", resp.StatusCode),
		}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, &iiqapi.Error{Kind: iiqapi.ErrorBodyRead, Status: resp.StatusCode, Err: err}
	}
	var result Response
	if err := json.Unmarshal(body, &result); err != nil {
		return Response{}, &iiqapi.Error{Kind: iiqapi.ErrorParse, Status: resp.StatusCode, Err: err}
	}
	result.Status = resp.StatusCode
	return result, nil
}

func addQuery(endpoint string, params url.Values) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	query := u.Query()
	for key, values := range params {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

var _ API = (*Client)(nil)
