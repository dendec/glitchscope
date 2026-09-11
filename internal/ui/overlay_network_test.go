package ui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestModArchiveCacheMissDoesNotBlockUI(t *testing.T) {
	entered := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-r.Context().Done()
	}))
	defer server.Close()
	o := &Overlay{baseDir: t.TempDir(), online: true}
	defer o.Close()
	done := make(chan []navEntry, 1)
	go func() { done <- o.buildModArchiveEntries(server.URL + "/directory/") }()
	select {
	case entries := <-done:
		if len(entries) != 1 || entries[0].kind != entryInfo {
			t.Fatalf("missing loading entry: %#v", entries)
		}
	case <-time.After(time.Second):
		t.Fatal("UI blocked on HTTP directory fetch")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("directory worker did not start")
	}
	o.modArchiveCancel()
	stopped := make(chan struct{})
	go func() { o.modArchiveWG.Wait(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("directory request ignored cancellation")
	}
}
