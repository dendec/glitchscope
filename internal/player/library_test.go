package player

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/dendec/glitchscope/internal/filesystem"
)

// writeFullWav creates a complete silent WAV file (0.1 s, mono, 44100 Hz,
// 16-bit) with actual sample data — unlike writeMinWav, this includes the
// data chunk so in-memory decoders (soloud.NewFfmpeg) can parse it.
func writeFullWav(t *testing.T, dir, name string) string {
	t.Helper()
	samples := 4410
	dataSize := samples * 2
	header := make([]byte, 44+dataSize)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+dataSize))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(header[22:24], 1) // mono
	binary.LittleEndian.PutUint32(header[24:28], 44100)
	binary.LittleEndian.PutUint32(header[28:32], 44100*2)
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

func TestGetAlbumTracksResolvesVirtualPaths(t *testing.T) {
	dir := t.TempDir()
	// Create a cached modland file (real WAV so metadata is extractable).
	cacheDir := filepath.Join(dir, ".cache", "modland", "files", "Format")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFullWav(t, cacheDir, "song.wav")

	lib := NewEmptyLibrary()
	lib.SetBaseDir(dir)
	lib.Albums = []Album{{
		Name:   "Format",
		Path:   ModlandPrefix + "Format",
		Tracks: []string{ModlandPrefix + "Format/song.wav"},
	}}

	infos := lib.GetAlbumTracks(0)
	if len(infos) != 1 {
		t.Fatalf("GetAlbumTracks returned %d infos, want 1", len(infos))
	}
	if !infos[0].Cached {
		t.Fatal("track should be marked as cached")
	}
	// Metadata must actually be extracted from the local file.
	if infos[0].Duration <= 0 {
		t.Fatalf("duration = %v, want > 0 (metadata not extracted)", infos[0].Duration)
	}
}

func TestScanLibraryAlbumsOK(t *testing.T) {
	dir := t.TempDir()
	// Track goes directly in the root: shouldSkipDir excludes some prefixes
	// (including /tmp) for *subdirectories*, so a nested fixture dir under
	// t.TempDir() would never be descended into.
	writeFullWav(t, dir, "track.wav")

	albums, status, err := ScanLibraryAlbums(dir)
	if err != nil {
		t.Fatal(err)
	}
	if status != filesystem.StatusOK {
		t.Fatalf("status = %v, want OK", status)
	}
	if len(albums) != 1 || len(albums[0].Tracks) != 1 {
		t.Fatalf("albums = %+v, want 1 album with 1 track", albums)
	}
}

func TestScanLibraryAlbumsMissingRoot(t *testing.T) {
	albums, status, err := ScanLibraryAlbums(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("expected an error for a missing root directory")
	}
	if status == filesystem.StatusOK {
		t.Fatal("expected a non-OK status for a missing root directory")
	}
	if albums != nil {
		t.Fatalf("expected nil albums on failure, got %+v", albums)
	}
}

func TestNewLibraryFromScan(t *testing.T) {
	albums := []Album{{Name: "A", Path: "/music/A", Tracks: []string{"/music/A/a.mp3"}}}
	lib := NewLibraryFromScan(albums)
	if lib.AlbumCount() != 1 {
		t.Fatalf("AlbumCount = %d, want 1", lib.AlbumCount())
	}
	if lib.CurrentAlbumIndex() != 0 || lib.CurrentTrackIndex() != 0 {
		t.Fatalf("cursor = (%d,%d), want (0,0)", lib.CurrentAlbumIndex(), lib.CurrentTrackIndex())
	}
}

func TestNewLibraryFromScanEmpty(t *testing.T) {
	lib := NewLibraryFromScan(nil)
	if lib.AlbumCount() != 0 {
		t.Fatalf("AlbumCount = %d, want 0", lib.AlbumCount())
	}
	if lib.CurrentAlbumIndex() != -1 {
		t.Fatalf("CurrentAlbumIndex = %d, want -1", lib.CurrentAlbumIndex())
	}
}

func TestGetAlbumTracksVirtualNotCached(t *testing.T) {
	dir := t.TempDir()
	lib := NewEmptyLibrary()
	lib.SetBaseDir(dir)
	lib.Albums = []Album{{
		Name:   "Test Album",
		Path:   ModlandPrefix + "Protracker",
		Tracks: []string{ModlandPrefix + "Protracker/song.mod"},
	}}

	infos := lib.GetAlbumTracks(0)
	if len(infos) != 1 {
		t.Fatalf("GetAlbumTracks returned %d infos, want 1", len(infos))
	}
	if infos[0].Cached {
		t.Fatal("track should NOT be marked as cached")
	}
	if infos[0].Duration != 0 {
		t.Fatalf("uncached track duration = %v, want 0", infos[0].Duration)
	}
}

func TestAddCatalogAlbumDedup(t *testing.T) {
	lib := NewEmptyLibrary()
	lib.Albums = []Album{
		{Name: "Local", Path: "/music/local", Tracks: []string{"/music/local/a.it"}},
	}

	catAlbum := Album{
		Name:   "ModArchive: test",
		Path:   ModArchivePrefix + "http://example.com/test",
		Tracks: []string{ModArchivePrefix + "http://example.com/test/track.mod"},
	}
	idx := lib.AddCatalogAlbum(catAlbum)
	if idx != 1 {
		t.Fatalf("AddCatalogAlbum returned %d, want 1", idx)
	}
	if len(lib.Albums) != 2 {
		t.Fatalf("Albums count = %d, want 2", len(lib.Albums))
	}

	// Adding the same album again should refresh it at the same stable index.
	updated := catAlbum
	updated.Tracks = append(updated.Tracks, ModArchivePrefix+"http://example.com/test/new.mod")
	idx2 := lib.AddCatalogAlbum(updated)
	if idx2 != 1 {
		t.Fatalf("AddCatalogAlbum dedup returned %d, want 1", idx2)
	}
	if len(lib.Albums) != 2 {
		t.Fatalf("Albums count after dedup = %d, want 2", len(lib.Albums))
	}
	if len(lib.Albums[1].Tracks) != 2 {
		t.Fatalf("updated catalog album tracks = %v", lib.Albums[1].Tracks)
	}
}
