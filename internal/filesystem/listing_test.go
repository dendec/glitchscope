package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWalkReportsSuccessAndLexicalOrder(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"z.mp3", "a.mp3"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var paths []string
	report := Walk(context.Background(), root, Options{}, func(entry Entry) { paths = append(paths, entry.Path) })
	if report.Status != StatusOK || report.Err() != nil {
		t.Fatalf("report = %+v, want successful report", report)
	}
	want := []string{filepath.Join(root, "a.mp3"), filepath.Join(root, "b"), filepath.Join(root, "z.mp3")}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("paths = %v, want %v", paths, want)
		}
	}
}

func TestWalkReportsPartialSubtree(t *testing.T) {
	root := t.TempDir()
	vanishing := filepath.Join(root, "vanishing")
	if err := os.Mkdir(vanishing, 0o755); err != nil {
		t.Fatal(err)
	}

	report := Walk(context.Background(), root, Options{}, func(entry Entry) {
		if entry.IsDir() {
			if err := os.RemoveAll(entry.Path); err != nil {
				t.Fatal(err)
			}
		}
	})
	if report.Status != StatusPartial {
		t.Fatalf("status = %v, want partial", report.Status)
	}
	if len(report.Issues) == 0 {
		t.Fatal("partial report should include issues")
	}
}

func TestWalkReportsFailedRoot(t *testing.T) {
	report := Walk(context.Background(), filepath.Join(t.TempDir(), "missing"), Options{}, func(Entry) {})
	if report.Status != StatusFailed {
		t.Fatalf("status = %v, want failed", report.Status)
	}
	if report.Err() == nil {
		t.Fatal("failed report should include an error")
	}
}

func TestWalkHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	report := Walk(ctx, root, Options{}, func(Entry) {})
	if report.Status != StatusFailed {
		t.Fatalf("status = %v, want failed cancellation", report.Status)
	}
	if report.Err() == nil {
		t.Fatal("cancelled walk should include an error")
	}
}
