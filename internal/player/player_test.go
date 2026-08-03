package player

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// writeMinWav creates a minimal silent WAV file (0.1 s, mono, 44100 Hz, 16-bit).
func writeMinWav(t *testing.T, dir, name string) string {
	t.Helper()
	samples := 4410
	byteRate := 44100 * 2
	dataSize := samples * 2
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+dataSize))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(header[22:24], 1) // mono
	binary.LittleEndian.PutUint32(header[24:28], 44100)
	binary.LittleEndian.PutUint32(header[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(header[32:34], 2) // block align
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(dataSize))

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, header, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func newTestPlayer(t *testing.T) *Player {
	t.Helper()
	p := &Player{pendingCh: make(chan loadResult, 1)}
	t.Cleanup(func() { p.Close() })
	return p
}

func TestPlayFileAsyncLocalSuccess(t *testing.T) {
	p := newTestPlayer(t)
	dir := t.TempDir()
	wav := writeMinWav(t, dir, "ok.wav")

	p.PlayFileAsync(wav)
	if !p.Loading() {
		t.Fatal("expected Loading() == true immediately after async start")
	}
	deadline := time.After(2 * time.Second)
	for {
		started, failed := p.CheckPending()
		if started || failed {
			if failed {
				t.Fatal("async load failed")
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for async load")
		default:
		}
	}
	if p.Loading() {
		t.Fatal("Loading() should be false after successful load")
	}
	if p.TrackPath() != wav {
		t.Fatalf("TrackPath = %q, want %q", p.TrackPath(), wav)
	}
}

func TestPlayFileAsyncDisplayPathPreserved(t *testing.T) {
	p := newTestPlayer(t)
	dir := t.TempDir()
	wav := writeMinWav(t, dir, "local.wav")

	virtualPath := "modland:mods/cool.mod"

	p.Downloader = func(path string, _ int64, _ func(int64, int64)) (string, error) {
		if path != virtualPath {
			t.Fatalf("Downloader got path %q, want %q", path, virtualPath)
		}
		return wav, nil
	}

	p.PlayFileAsync(virtualPath)
	deadline := time.After(2 * time.Second)
	for {
		started, _ := p.CheckPending()
		if started {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out")
		default:
		}
	}
	if p.TrackPath() != virtualPath {
		t.Fatalf("TrackPath = %q, want virtual path %q", p.TrackPath(), virtualPath)
	}
}

func TestPlayFileAsyncNewerRequestWins(t *testing.T) {
	p := newTestPlayer(t)
	dir := t.TempDir()
	wav1 := writeMinWav(t, dir, "old.wav")
	wav2 := writeMinWav(t, dir, "new.wav")

	var block1, block2 sync.WaitGroup
	block1.Add(1)
	block2.Add(1)

	callCount := atomic.Int32{}

	p.loadFunc = func(localPath string) loadResult {
		n := int(callCount.Add(1))

		var block *sync.WaitGroup
		if n == 1 {
			block = &block1
		} else {
			block = &block2
		}
		block.Wait()

		return loadResult{path: localPath, localPath: localPath, duration: 1.0}
	}

	p.PlayFileAsync(wav1)
	p.PlayFileAsync(wav2)
	block1.Done()
	block2.Done()

	deadline := time.After(2 * time.Second)
	for {
		started, _ := p.CheckPending()
		if started {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for newer request")
		default:
		}
	}

	if p.TrackPath() != wav2 {
		t.Fatalf("TrackPath = %q, want %q (newer request should win)", p.TrackPath(), wav2)
	}
	if p.Loading() {
		t.Fatal("Loading() should be false")
	}
}

func TestPlayFileAsyncDownloaderErrorClearsLoading(t *testing.T) {
	p := newTestPlayer(t)

	p.Downloader = func(_ string, _ int64, _ func(int64, int64)) (string, error) {
		return "", &testErr{msg: "download failed"}
	}

	p.PlayFileAsync("modland:broken.mod")
	deadline := time.After(2 * time.Second)
	for {
		_, failed := p.CheckPending()
		if failed {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for error result")
		default:
		}
	}
	if p.Loading() {
		t.Fatal("Loading() should be false after download error")
	}
}

func TestPlayFileAsyncStopDuringLoad(t *testing.T) {
	p := newTestPlayer(t)
	dir := t.TempDir()
	wav := writeMinWav(t, dir, "slow.wav")

	started := make(chan struct{})
	block := make(chan struct{})

	p.loadFunc = func(localPath string) loadResult {
		close(started)
		<-block
		return loadResult{path: localPath, localPath: localPath, duration: 1.0}
	}

	p.PlayFileAsync(wav)
	<-started
	p.Stop()

	if p.Loading() {
		t.Fatal("Loading() should be false after Stop")
	}
	if p.TrackPath() != "" {
		t.Fatalf("TrackPath = %q after Stop, want empty", p.TrackPath())
	}
	close(block)

	// Let the stale goroutine finish and try to publish.
	deadline := time.After(2 * time.Second)
	for i := 0; i < 10; i++ {
		p.CheckPending()
		select {
		case <-deadline:
			break
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if p.TrackPath() != "" {
		t.Fatalf("TrackPath = %q after stale result, should remain empty", p.TrackPath())
	}
}

func TestPlayFileAsyncProgressNotOverwritten(t *testing.T) {
	p := newTestPlayer(t)

	// Scenario: old worker finishes download and enters decode phase.
	// Its loadPercent.Store(-1) must not clobber new request's progress.

	dl1Started := make(chan struct{})
	dl1DownloadDone := make(chan struct{}) // download finished, about to enter decode
	dl1Proceed := make(chan struct{})       // let old worker finish

	p.Downloader = func(_ string, _ int64, onProgress func(int64, int64)) (string, error) {
		close(dl1Started)
		// Emit some progress.
		onProgress(50, 100)
		<-dl1DownloadDone
		return "/dev/null", nil
	}
	p.loadFunc = func(localPath string) loadResult {
		// This is the "decode phase" — old worker would reset loadPercent here.
		<-dl1Proceed
		return loadResult{path: localPath, localPath: localPath, duration: 1.0}
	}

	// Start first (slow) download.
	p.PlayFileAsync("modland:slow.mod")
	<-dl1Started

	// Let first download finish — goroutine now enters decode phase (loadFunc).
	close(dl1DownloadDone)

	// Start second request — its downloader immediately reports progress.
	dl2Started := make(chan struct{})
	dl2Done := make(chan struct{})
	p.Downloader = func(_ string, _ int64, onProgress func(int64, int64)) (string, error) {
		close(dl2Started)
		// New request gets progress quickly.
		onProgress(75, 100)
		<-dl2Done
		return "/dev/null", nil
	}
	p.PlayFileAsync("modland:other.mod")
	<-dl2Started

	// At this point: new request has progress 75.
	// Old worker is stuck in loadFunc — when unblocked, it will NOT reset
	// loadPercent because requestID check guards it.
	pct1 := p.loadPercent.Load()
	if pct1 != 75 {
		t.Fatalf("progress after second request = %d, want 75", pct1)
	}

	// Unblock old worker. Its decode-phase reset must be guarded.
	close(dl1Proceed)
	time.Sleep(30 * time.Millisecond)

	pct2 := p.loadPercent.Load()
	if pct2 != 75 {
		t.Fatalf("progress after old worker decode = %d, want 75 (stale reset must not clobber)", pct2)
	}

	close(dl2Done)
}

func TestPlayFileAsyncLocalPathUsedForBitrate(t *testing.T) {
	p := newTestPlayer(t)
	dir := t.TempDir()
	wav := writeMinWav(t, dir, "bitrate.wav")

	virtualPath := "modarchive:mods/test.mod"

	p.Downloader = func(_ string, _ int64, _ func(int64, int64)) (string, error) {
		return wav, nil
	}

	p.PlayFileAsync(virtualPath)
	deadline := time.After(2 * time.Second)
	for {
		started, _ := p.CheckPending()
		if started {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out")
		default:
		}
	}

	// Bitrate should be computed from the downloaded file, not the virtual path.
	if p.Bitrate() < 0 {
		t.Fatalf("Bitrate = %f, want non-negative", p.Bitrate())
	}
}

func TestCloseWaitsForWorkers(t *testing.T) {
	p := newTestPlayer(t)

	dlStarted := make(chan struct{})
	dlDone := make(chan struct{})

	p.Downloader = func(_ string, _ int64, _ func(int64, int64)) (string, error) {
		close(dlStarted)
		<-dlDone // slow download — goroutine alive until unblocked
		return "/dev/null", nil
	}

	p.PlayFileAsync("test.wav")
	<-dlStarted

	closeDone := make(chan struct{})
	go func() {
		p.Close()
		close(closeDone)
	}()

	// Close must NOT return while Downloader is blocked.
	select {
	case <-closeDone:
		t.Fatal("Close returned while worker still running")
	case <-time.After(50 * time.Millisecond):
	}

	close(dlDone) // unblock worker

	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after worker finished")
	}
}

func TestPlayFileAsyncRejectsAfterClose(t *testing.T) {
	p := newTestPlayer(t)
	dir := t.TempDir()
	wav := writeMinWav(t, dir, "after.wav")

	p.Close()

	p.PlayFileAsync(wav)
	if p.Loading() {
		t.Fatal("PlayFileAsync after Close should not start loading")
	}
}

func TestCheckPendingReturnsCorrectState(t *testing.T) {
	p := newTestPlayer(t)

	started, failed := p.CheckPending()
	if started || failed {
		t.Fatalf("empty CheckPending: started=%v, failed=%v, want false,false", started, failed)
	}

	dir := t.TempDir()
	wav := writeMinWav(t, dir, "state.wav")

	p.PlayFileAsync(wav)
	deadline := time.After(2 * time.Second)
	for {
		started, failed = p.CheckPending()
		if started || failed {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out")
		default:
		}
	}
	if failed {
		t.Fatal("unexpected failure")
	}
	if !started {
		t.Fatal("expected started == true")
	}
}

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }
