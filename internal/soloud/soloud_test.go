package soloud

import (
	"reflect"
	"testing"
)

func TestInterleavedToPlanar(t *testing.T) {
	interleaved := []float32{
		1, 10, 100,
		2, 20, 200,
		3, 30, 300,
	}
	want := []float32{
		1, 2, 3,
		10, 20, 30,
		100, 200, 300,
	}

	if got := interleavedToPlanar(interleaved, 3); !reflect.DeepEqual(got, want) {
		t.Errorf("interleavedToPlanar() = %v, want %v", got, want)
	}
}