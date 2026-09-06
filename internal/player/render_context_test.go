package player

import (
	"context"
	"errors"
	"testing"

	"github.com/dendec/glitchscope/internal/openmpt"
	"github.com/dendec/glitchscope/internal/soloud"
	"github.com/dendec/glitchscope/internal/xmp"
)

// Cancels through a real context at a deterministic decoder-block boundary.
type blockCancelContext struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (c *blockCancelContext) Err() error {
	c.checks++
	if c.checks == 3 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestTrackerRenderingChecksCancellationBetweenBlocks(t *testing.T) {
	// One valid, silent ProTracker pattern, with no sample payloads.
	data := make([]byte, 1084+1024)
	data[950] = 1
	copy(data[1080:], "M.K.")
	for name, render := range map[string]func(context.Context, []byte, int, int) ([]float32, int, int, error){
		"openmpt": openmpt.RenderContext, "xmp": xmp.RenderContext,
	} {
		t.Run(name, func(t *testing.T) {
			samples, channels, frames, err := render(context.Background(), data, 44100, 8192)
			if err != nil || frames != 8192 || channels != 2 || len(samples) != frames*channels {
				t.Fatalf("render = %d samples, %d ch, %d frames, %v", len(samples), channels, frames, err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cancelled := &blockCancelContext{Context: ctx, cancel: cancel}
			samples, _, _, err = render(cancelled, data, 44100, 44100)
			if !errors.Is(err, context.Canceled) || samples != nil {
				t.Fatalf("cancelled render = %d samples, %v", len(samples), err)
			}
		})
	}
}

func TestChipCancellationBeforeNativeAllocation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, render := range map[string]func(context.Context, []byte, int) ([]float32, int, int, error){
		"YM": soloud.RenderYmContext, "SID": soloud.RenderSidContext,
	} {
		t.Run(name, func(t *testing.T) {
			samples, _, _, err := render(ctx, nil, 44100*360)
			if !errors.Is(err, context.Canceled) || samples != nil {
				t.Fatalf("cancelled render = %d samples, %v", len(samples), err)
			}
		})
	}
}
