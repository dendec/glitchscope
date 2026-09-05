package util

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

const (
	httpMaxAttempts    = 3
	httpRequestTimeout = 5 * time.Second
	httpRetryDelay     = 250 * time.Millisecond
)

var httpClient = &http.Client{Transport: defaultHTTPTransport()}

func defaultHTTPTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: httpRequestTimeout}).DialContext
	transport.TLSHandshakeTimeout = httpRequestTimeout
	transport.ResponseHeaderTimeout = httpRequestTimeout
	return transport
}

// Get performs an HTTP GET with bounded retries for temporary failures.
// The request context controls the total operation; each attempt is separately
// limited so an unreachable server does not consume the entire operation time.
func Get(ctx context.Context, targetURL string, headers http.Header) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header = headers.Clone()
		resp, err := httpClient.Do(req)
		if err == nil && (resp.StatusCode != http.StatusRequestTimeout && resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < http.StatusInternalServerError) {
			return resp, nil
		}
		if resp != nil {
			err = fmt.Errorf("temporary HTTP status %d", resp.StatusCode)
			resp.Body.Close()
		}
		if attempt == httpMaxAttempts-1 {
			return nil, err
		}
		timer := time.NewTimer(httpRetryDelay * time.Duration(1<<attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
