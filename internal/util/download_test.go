package util

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadWithFallback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("download content ok"))
	}))
	defer ts.Close()

	tmpDir, err := os.MkdirTemp("", "util_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	targetPath := filepath.Join(tmpDir, "file.dat")
	urls := []string{
		ts.URL + "/fail",
		ts.URL + "/success",
	}

	var bytesRead int64
	err = DownloadWithFallback(context.Background(), urls, targetPath, 0, func(read, total int64) {
		bytesRead = read
	})
	if err != nil {
		t.Fatalf("DownloadWithFallback failed: %v", err)
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(content) != "download content ok" {
		t.Errorf("unexpected content: %q", string(content))
	}
	if bytesRead != int64(len("download content ok")) {
		t.Errorf("unexpected bytesRead: %d", bytesRead)
	}
}
