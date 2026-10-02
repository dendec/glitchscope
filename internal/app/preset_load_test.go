package app

import (
	"context"
	"errors"
	"testing"
)

func TestPresetLoadIgnoresStaleResult(t *testing.T) {
	a := &App{
		presetLoadResults:  make(chan presetLoadResult, 2),
		presetLoadInFlight: true,
	}
	a.presetRequestID.Store(2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.presetLoadCancel = cancel
	a.presetLoadResults <- presetLoadResult{requestID: 1, err: errors.New("old failure")}

	a.pollPresetLoad()

	if !a.presetLoadInFlight || ctx.Err() != nil {
		t.Fatal("stale result cancelled the current request")
	}
}

func TestPresetLoadFailureReleasesContext(t *testing.T) {
	a := &App{
		presetLoadResults:  make(chan presetLoadResult, 2),
		presetLoadInFlight: true,
	}
	a.presetRequestID.Store(2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.presetLoadCancel = cancel
	a.presetLoadResults <- presetLoadResult{requestID: 1}
	a.presetLoadResults <- presetLoadResult{requestID: 2, err: errors.New("read failed")}

	a.pollPresetLoad()

	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("completed reader context was not released")
	}
	if a.presetLoadInFlight || a.presetLoadData != nil || a.presetLoadCancel != nil {
		t.Fatal("failed load left pending state")
	}
}

func TestCancelPresetLoadInvalidatesQueuedResult(t *testing.T) {
	a := &App{
		presetLoadResults:  make(chan presetLoadResult, 1),
		presetLoadInFlight: true,
		presetLoadFrames:   3,
		presetLoadData:     &presetLoadResult{name: "removed.milk"},
	}
	a.presetRequestID.Store(7)
	ctx, cancel := context.WithCancel(context.Background())
	a.presetLoadCancel = cancel

	a.cancelPresetLoad()

	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("pending preset reader was not cancelled")
	}
	if a.presetRequestID.Load() != 8 || a.presetLoadInFlight || a.presetLoadFrames != 0 || a.presetLoadData != nil {
		t.Fatal("cancel did not invalidate pending preset state")
	}
	a.presetLoadResults <- presetLoadResult{requestID: 7, name: "removed.milk"}
	a.pollPresetLoad()
	if a.presetLoadData != nil || a.presetLoadInFlight {
		t.Fatal("stale result was applied after cancellation")
	}
}
