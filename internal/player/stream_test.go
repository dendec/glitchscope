package player

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/dendec/glitchscope/internal/soloud"
)

func TestRadioOpenCancelledByStop(t *testing.T) {
	p := newTestPlayer(t)
	entered, cancelled := make(chan struct{}), make(chan struct{})
	p.OpenStream = func(ctx context.Context, _ string) (io.ReadCloser, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}
	p.PlayFileAsync("radio:stalled")
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("open did not start")
	}
	p.Stop()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("Stop failed to cancel stream open")
	}
	p.loadWG.Wait()
	if started, failed := p.CheckPending(); started || failed {
		t.Fatal("cancelled load published a result")
	}
}

func TestStreamPrebufferAcceptsCompletedAudio(t *testing.T) {
	// One second of PCM WAV: decoding can finish before the first status poll.
	data := make([]byte, 44+44100*2)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 44100)
	binary.LittleEndian.PutUint32(data[28:], 88200)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(len(data)-44))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := loadStreamContext(ctx, io.NopCloser(bytes.NewReader(data)), func() {})
	if result.err != nil {
		t.Fatal(result.err)
	}
	defer result.src.Destroy()
	stream := result.src.(*soloud.FfmpegStream)
	if stream.BufferedFrames() < streamPrebufferFrames {
		t.Fatal("missing PCM prebuffer")
	}
}

func TestStreamPrebufferCancelsStalledBody(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan loadResult, 1)
	go func() { done <- loadStreamContext(ctx, reader, func() {}) }()
	select {
	case result := <-done:
		if result.err == nil {
			result.src.Destroy()
			t.Fatal("stalled body unexpectedly loaded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream teardown hung on input")
	}
}

func TestStopDoesNotWaitForStreamTeardown(t *testing.T) {
	p := newTestPlayer(t)
	stream, err := soloud.NewFfmpegStream()
	if err != nil {
		t.Fatal(err)
	}
	release, entered := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	stream.SetOnDestroy(func() { close(entered); <-release })
	p.current = stream
	done := make(chan struct{})
	go func() { p.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop waited for stream teardown")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("stream teardown did not start")
	}
	once.Do(func() { close(release) })
	p.streamWG.Wait()
}

func TestRadioSeekNeverRestartsLiveSource(t *testing.T) {
	p := newTestPlayer(t)
	p.currentPath = "radio:live"
	p.pos = 30
	if err := p.Seek(0); err == nil {
		t.Fatal("live source accepted a seek")
	}
	if p.pos != 30 {
		t.Fatal("live clock was reset by seek")
	}
}
