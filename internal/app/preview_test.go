package app

import (
	"testing"
	"time"
)

func TestPreviewFrameDue(t *testing.T) {
	t.Parallel()

	now := time.Unix(100, 0)
	if previewFrameDue(now, now.Add(previewFramePeriod)) {
		t.Fatal("previewFrameDue() = true before deadline")
	}
	if !previewFrameDue(now, now) {
		t.Fatal("previewFrameDue() = false at deadline")
	}
	if !previewFrameDue(now, time.Time{}) {
		t.Fatal("previewFrameDue() = false without a deadline")
	}
}

func TestPreviewSelectionKeepsActiveUntilCommit(t *testing.T) {
	r := &previewRenderer{active: &previewJob{key: "active"}}
	if !r.selectJob("next", "next data") || r.active.key != "active" {
		t.Fatal("new selection discarded the active preview")
	}
	if !r.selectJob("latest", "latest data") || len(r.queue) != 1 || r.queue[0].key != "latest" {
		t.Fatal("rapid selection did not replace the queued job")
	}
	r.loading = &r.queue[0]
	r.queue = nil
	if r.selectJob("latest", "same data") || r.loading == nil {
		t.Fatal("repeated selection restarted pending compilation")
	}
	if !r.selectJob("active", "old data") || r.loading != nil || len(r.queue) != 0 {
		t.Fatal("returning to active preset did not cancel pending compilation")
	}
	if r.selectJob("active", "old data") {
		t.Fatal("active preset was unnecessarily reloaded")
	}
}
