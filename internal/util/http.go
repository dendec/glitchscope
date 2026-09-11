package util

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/dendec/glitchscope/internal/version"
)

const (
	httpMaxAttempts     = 3
	httpRequestTimeout  = 5 * time.Second
	httpRetryDelay      = 250 * time.Millisecond
	httpHeaderTimeout   = 15 * time.Second
	httpBodyReadTimeout = 30 * time.Second
)

var httpClient = &http.Client{Transport: defaultHTTPTransport()}

func defaultHTTPTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: httpRequestTimeout}).DialContext
	transport.TLSHandshakeTimeout = httpRequestTimeout
	// The shared transport also serves long-lived ICY metadata streams. Short
	// operations use their context to impose a tighter deadline.
	transport.ResponseHeaderTimeout = httpHeaderTimeout
	return transport
}

// Get performs an HTTP GET with bounded retries for temporary failures.
// The request context controls the total operation. The shared transport
// bounds connection setup and response-header waits for every caller.
func Get(ctx context.Context, targetURL string, headers http.Header) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		requestCtx, cancel := context.WithCancel(ctx)
		req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, targetURL, nil)
		if err != nil {
			cancel()
			return nil, err
		}
		req.Header = headers.Clone()
		if req.Header == nil {
			req.Header = make(http.Header)
		}
		req.Header.Set("User-Agent", version.UserAgent())
		resp, err := httpClient.Do(req)
		if err == nil && (resp.StatusCode != http.StatusRequestTimeout && resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < http.StatusInternalServerError) {
			resp.Body = &idleTimeoutBody{ReadCloser: resp.Body, cancel: cancel, timeout: httpBodyReadTimeout}
			return resp, nil
		}
		cancel()
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

// Time only an outstanding Read, not time spent paused by the consumer. This
// bounds stalled bodies without imposing a lifetime limit on radio streams.
type idleTimeoutBody struct {
	io.ReadCloser
	cancel  context.CancelFunc
	timeout time.Duration
}

func (b *idleTimeoutBody) Read(p []byte) (int, error) {
	timer := time.AfterFunc(b.timeout, b.cancel)
	defer timer.Stop()
	return b.ReadCloser.Read(p)
}

func (b *idleTimeoutBody) Close() error {
	b.cancel()
	return b.ReadCloser.Close()
}
