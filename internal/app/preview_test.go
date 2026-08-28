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
