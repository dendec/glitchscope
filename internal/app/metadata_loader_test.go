package app

import (
	"context"
	"testing"
	"time"

	"github.com/dendec/glitchscope/internal/player"
)

func TestMetadataLoaderCancelsAndRejectsStaleResults(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	l := newMetadataLoader(context.Background(), func(ctx context.Context, album player.Album) metadataResult {
		started <- album.Path
		select {
		case <-ctx.Done():
		case <-release:
		}
		return metadataResult{infos: []player.TrackInfo{{Path: album.Tracks[0]}}}
	})
	defer l.close()
	album := player.Album{Path: "old", Tracks: []string{"original"}}
	l.request(album)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	// The worker must own a snapshot, not read mutable UI/library slices.
	album.Tracks[0] = "mutated"
	l.request(player.Album{Path: "new", Tracks: []string{"new-track"}})
	select {
	case path := <-started:
		if path != "new" {
			t.Fatal(path)
		}
	case <-time.After(time.Second):
		t.Fatal("superseded request did not cancel")
	}
	close(release)
	select {
	case result := <-l.results:
		if result.id != l.requestID.Load() || result.infos[0].Path != "new-track" {
			t.Fatal("stale metadata published")
		}
	case <-time.After(time.Second):
		t.Fatal("new result not published")
	}
	// Even a result queued before cancellation must not reach the overlay.
	l.results <- metadataResult{id: l.requestID.Load()}
	l.invalidate()
	if _, ok := l.poll(); ok {
		t.Fatal("accepted an invalidated result")
	}
}

func TestMetadataLoaderOwnsTrackSnapshotAndCloseWaits(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	l := newMetadataLoader(context.Background(), func(_ context.Context, album player.Album) metadataResult {
		close(started)
		<-release
		return metadataResult{infos: []player.TrackInfo{{Path: album.Tracks[0]}}}
	})
	album := player.Album{Tracks: []string{"original"}}
	l.request(album)
	<-started
	album.Tracks[0] = "mutated"
	close(release)
	select {
	case result := <-l.results:
		if result.infos[0].Path != "original" {
			t.Fatal("worker read mutable tracks")
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not finish")
	}
	l.close()
	if l.ctx.Err() == nil {
		t.Fatal("shutdown did not cancel worker")
	}
}
