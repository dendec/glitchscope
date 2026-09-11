package util

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dendec/glitchscope/internal/version"
)

func TestGetSetsApplicationUserAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("User-Agent"), version.UserAgent(); got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	response, err := Get(context.Background(), server.URL, nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
}

func TestGetBoundsStalledBodyButAllowsPausedConsumer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	response, err := Get(context.Background(), server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body := response.Body.(*idleTimeoutBody)
	body.timeout = 20 * time.Millisecond
	time.Sleep(40 * time.Millisecond) // no Read: a paused stream must stay alive
	done := make(chan error, 1)
	go func() { _, err := body.Read(make([]byte, 1)); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("stalled Read succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("stalled Read was not cancelled")
	}
}
