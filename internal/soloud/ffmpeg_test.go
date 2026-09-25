package soloud

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// TestFfmpegFileMatchesMemory verifies that the file-backed (streaming) loader
// produces the same metadata as the in-memory loader for the same files. The
// file path exercises the custom AVIO read/seek callbacks reading from disk.
func TestFfmpegFileMatchesMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tone.wav")
	data := testPCM16WAV()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write WAV fixture: %v", err)
	}

	fileSrc, err := NewFfmpegFile(path)
	if err != nil {
		t.Fatalf("NewFfmpegFile: %v", err)
	}
	defer fileSrc.Destroy()

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
}

func testPCM16WAV() []byte {
	const (
		channels      = 2
		sampleRate    = 44100
		frames        = 4410
		bitsPerSample = 16
	)
	const bytesPerFrame = channels * bitsPerSample / 8
	const dataSize = frames * bytesPerFrame

	data := make([]byte, 44+dataSize)
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
	copy(data[8:16], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:20], 16)
	binary.LittleEndian.PutUint16(data[20:22], 1)
	binary.LittleEndian.PutUint16(data[22:24], channels)
	binary.LittleEndian.PutUint32(data[24:28], sampleRate)
	binary.LittleEndian.PutUint32(data[28:32], sampleRate*bytesPerFrame)
	binary.LittleEndian.PutUint16(data[32:34], bytesPerFrame)
	binary.LittleEndian.PutUint16(data[34:36], bitsPerSample)
	copy(data[36:40], "data")
	binary.LittleEndian.PutUint32(data[40:44], dataSize)
	return data
}
