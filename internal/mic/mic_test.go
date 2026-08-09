package mic

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestToMonoS16Stereo(t *testing.T) {
	// Two frames: (L=-16384, R=16384) → 0; (L=0, R=16384) → 0.25.
	data := make([]byte, 8)
	binary.NativeEndian.PutUint16(data[0:], 0xC000) // -16384
	binary.NativeEndian.PutUint16(data[2:], 0x4000) // +16384
	binary.NativeEndian.PutUint16(data[4:], 0)
	binary.NativeEndian.PutUint16(data[6:], 0x4000)
	got := ToMono(data, false, 2)
	want := []float32{0, 0.25}
	if len(got) != 2 || math.Abs(float64(got[0]-want[0])) > 1e-5 || math.Abs(float64(got[1]-want[1])) > 1e-5 {
		t.Fatalf("ToMono(s16,stereo) = %v, want %v", got, want)
	}
}

func TestToMonoF32Mono(t *testing.T) {
	data := make([]byte, 8)
	binary.NativeEndian.PutUint32(data[0:], math.Float32bits(-0.5))
	binary.NativeEndian.PutUint32(data[4:], math.Float32bits(0.5))
	got := ToMono(data, true, 1)
	if len(got) != 2 || got[0] != -0.5 || got[1] != 0.5 {
		t.Fatalf("ToMono(f32,mono) = %v, want [-0.5 0.5]", got)
	}
}

func TestToMonoEmpty(t *testing.T) {
	if got := ToMono(nil, false, 2); got != nil {
		t.Fatalf("ToMono(nil) = %v, want nil", got)
	}
}
