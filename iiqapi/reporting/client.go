package reporting

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/Intent-IQ/identity-go/iiqapi"
)

type Client struct{ httpClient *http.Client }

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{httpClient: httpClient}
}

func (c *Client) ReportImpression(ctx context.Context, requestURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return &iiqapi.Error{Kind: iiqapi.ErrorRequest, Err: err}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		kind := iiqapi.ErrorTransport
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			kind = iiqapi.ErrorTimeout
		}
		return &iiqapi.Error{Kind: kind, Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		return &iiqapi.Error{
			Kind:   iiqapi.ErrorStatus,
			Status: resp.StatusCode,
			Err:    fmt.Errorf("reporting API returned %d: %s", resp.StatusCode, body),
		}
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

var _ API = (*Client)(nil)
