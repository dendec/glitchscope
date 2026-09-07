package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestConnectivityCachesBothResultsForOneMinute(t *testing.T) {
	now := time.Unix(100, 0)
	var calls atomic.Int32
	online := false
	c := &connectivityCache{
		now: func() time.Time { return now },
		probe: func(context.Context) bool {
			calls.Add(1)
			return online
		},
	}
	if c.Check(context.Background()) {
		t.Fatal("first check unexpectedly online")
	}
	online = true
	if c.Check(context.Background()) {
		t.Fatal("offline result was not cached")
	}
	if calls.Load() != 1 {
		t.Fatalf("probe calls = %d, want 1", calls.Load())
	}
	now = now.Add(time.Minute)
	if !c.Check(context.Background()) {
		t.Fatal("expired result was not refreshed")
	}
	if calls.Load() != 2 {
		t.Fatalf("probe calls = %d, want 2", calls.Load())
	}
}

func TestConnectivityCoalescesConcurrentChecks(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	c := &connectivityCache{
		now: time.Now,
		probe: func(context.Context) bool {
			calls.Add(1)
			close(started)
			<-release
			return true
		},
	}
	results := make(chan bool, 2)
	go func() { results <- c.Check(context.Background()) }()
	<-started
	go func() { results <- c.Check(context.Background()) }()
	close(release)
	first := <-results
	second := <-results
	if !first || !second {
		t.Fatal("coalesced checks should return online")
	}
	if calls.Load() != 1 {
		t.Fatalf("probe calls = %d, want 1", calls.Load())
	}
}
