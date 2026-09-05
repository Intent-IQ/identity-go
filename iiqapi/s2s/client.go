package s2s

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	iiqapi "github.com/Intent-IQ/identity-go/iiqapi"
)

const maxErrorSnippetSize = 1024

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
		req.Header.Set("gdpr-consent", consent)
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

var _ API = (*Client)(nil)
