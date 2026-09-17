//go:build !linux

package mic

import (
	"fmt"
	"time"
)

// alsaCap is a placeholder on non-Linux platforms. SDL remains the portable
// microphone backend; Open falls through when SDL capture is unavailable.
type alsaCap struct{}

func openALSA() (*alsaCap, error) {
	return nil, fmt.Errorf("ALSA capture is unavailable on this platform")
}

func (a *alsaCap) probe(time.Duration) (int, bool, int, int) {
	return 0, false, 0, 0
}

func (a *alsaCap) read() []byte { return nil }

func (a *alsaCap) Close() {}
