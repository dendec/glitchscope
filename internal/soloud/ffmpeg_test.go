package soloud

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFfmpegFileMatchesMemory verifies that the file-backed (streaming) loader
// produces the same metadata as the in-memory loader for the same files. The
// file path exercises the custom AVIO read/seek callbacks reading from disk.
func TestFfmpegFileMatchesMemory(t *testing.T) {
	files := []string{
		"../../test_data/ffmpeg/pcm.wav",
		"../../test_data/ffmpeg/mpeg-layer3.mp3",
		"../../test_data/ffmpeg/flac.flac",
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			fileSrc, err := NewFfmpegFile(path)
			if err != nil {
				t.Fatalf("NewFfmpegFile: %v", err)
			}
			defer fileSrc.Destroy()

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			memSrc, err := NewFfmpeg(data)
			if err != nil {
				t.Fatalf("NewFfmpeg: %v", err)
			}
			defer memSrc.Destroy()

			if fileSrc.GetLength() <= 0 {
				t.Errorf("expected positive length, got %v", fileSrc.GetLength())
			}
			if got, want := fileSrc.GetLength(), memSrc.GetLength(); got != want {
				t.Errorf("length: file-backed = %v, memory = %v", got, want)
			}
			if got, want := fileSrc.GetChannels(), memSrc.GetChannels(); got != want {
				t.Errorf("channels: file-backed = %v, memory = %v", got, want)
			}
		})
	}
}
