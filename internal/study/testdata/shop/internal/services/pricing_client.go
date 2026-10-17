package services

import (
	"context"
	"net/http"
	"strconv"
)

// PricingClient asks an external API for the overage rate.
type PricingClient struct {
	HTTP *http.Client
	URL  string
}

// OverageRate fetches the rate per unit above the quota.
func (c *PricingClient) OverageRate(ctx context.Context) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return strconv.ParseInt(resp.Header.Get("X-Rate"), 10, 64)
}
