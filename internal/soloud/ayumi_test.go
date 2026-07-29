package soloud

import (
	"encoding/binary"
	"testing"
)

func TestNewAyumiWithRawVTX(t *testing.T) {
	const frames = 50
	data := make([]byte, 16+5+14*frames)
	data[0], data[1] = 'a', 'y'
	data[2] = 1 // ABC stereo
	binary.LittleEndian.PutUint32(data[5:], 1773400)
	data[9] = 50
	binary.LittleEndian.PutUint32(data[12:], 14*frames)

	ayumi, err := NewAyumi(data)
	if err != nil {
		t.Fatal(err)
	}
	defer ayumi.Destroy()

	if ayumi.GetLength() != 1 {
		t.Fatalf("length = %f, want 1", ayumi.GetLength())
	}
	if ayumi.GetSampleRate() != 44100 {
		t.Fatalf("sample rate = %d, want 44100", ayumi.GetSampleRate())
	}
}

func TestNewAyumiRejectsCompressedVTXPayload(t *testing.T) {
	data := make([]byte, 21)
	data[0], data[1] = 'a', 'y'
	binary.LittleEndian.PutUint32(data[5:], 1773400)
	data[9] = 50
	binary.LittleEndian.PutUint32(data[12:], 14)
	data[16] = 1 // Missing the five NUL-terminated metadata strings.

	if _, err := NewAyumi(data); err == nil {
		t.Fatal("NewAyumi accepted malformed VTX metadata")
	}
}
