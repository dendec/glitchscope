package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dendec/glitchscope/internal/util"
)

const (
	connectivityTTL     = time.Minute
	connectivityTimeout = 5 * time.Second
	connectivityProbe   = "https://modland.antarctica.no/"
)

// connectivityCache performs probes only on demand, coalesces concurrent
// callers, and caches both online and offline results.
type connectivityCache struct {
	mu        sync.Mutex
	checkedAt time.Time
	online    bool
	inFlight  chan struct{}
	now       func() time.Time
	probe     func(context.Context) bool
}

func newConnectivityCache() *connectivityCache {
	c := &connectivityCache{now: time.Now}
	c.probe = c.probeNetwork
	return c
}

func (c *connectivityCache) Check(ctx context.Context) bool {
	for {
		c.mu.Lock()
		if !c.checkedAt.IsZero() && c.now().Sub(c.checkedAt) < connectivityTTL {
			online := c.online
			c.mu.Unlock()
			return online
		}
		if c.inFlight != nil {
			done := c.inFlight
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return false
			case <-done:
				continue
			}
		}
		done := make(chan struct{})
		c.inFlight = done
		c.mu.Unlock()

		online := c.probe(ctx)
		c.mu.Lock()
		c.online = online
		c.checkedAt = c.now()
		c.inFlight = nil
		close(done)
		c.mu.Unlock()
		return online
	}
}

func (c *connectivityCache) probeNetwork(parent context.Context) bool {
	ctx, cancel := context.WithTimeout(parent, connectivityTimeout)
	defer cancel()
	resp, err := util.Get(ctx, connectivityProbe, nil)
	if err != nil {
		slog.Warn("connectivity probe failed", "error", err)
		return false
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Debug("connectivity response close", "error", err)
		}
	}()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

func requireOnline(c *connectivityCache, ctx context.Context) error {
	if c.Check(ctx) {
		return nil
	}
	return fmt.Errorf("network unavailable")
}
