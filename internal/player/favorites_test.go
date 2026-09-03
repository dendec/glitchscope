package player

import (
	"os"
	"path/filepath"
	"testing"
)

// mustCycle is a test helper that cycles and fails on error.
func mustCycle(t *testing.T, f *Favorites, track string) PlaylistID {
	t.Helper()
	kind, err := f.Cycle(track)
	if err != nil {
		t.Fatalf("Cycle(%q): %v", track, err)
	}
	return kind
}

func TestFavoritesCycle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "favorites.json")
	track := filepath.Join(dir, "music", "test.mod")

	f, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}

	// None → Star
	kind := mustCycle(t, f, track)
	if kind != PlaylistStar {
		t.Fatalf("first cycle: got %q, want %q", kind, PlaylistStar)
	}
	if f.Count(PlaylistStar) != 1 {
		t.Fatalf("star count: got %d, want 1", f.Count(PlaylistStar))
	}

	// Star → Heart
	kind = mustCycle(t, f, track)
	if kind != PlaylistHeart {
		t.Fatalf("second cycle: got %q, want %q", kind, PlaylistHeart)
	}
	if f.Count(PlaylistStar) != 0 {
		t.Fatalf("star count after move: got %d, want 0", f.Count(PlaylistStar))
	}
	if f.Count(PlaylistHeart) != 1 {
		t.Fatalf("heart count: got %d, want 1", f.Count(PlaylistHeart))
	}

	// Heart → Note
	kind = mustCycle(t, f, track)
	if kind != PlaylistNote {
		t.Fatalf("third cycle: got %q, want %q", kind, PlaylistNote)
	}
	if f.Count(PlaylistHeart) != 0 {
		t.Fatalf("heart count after move: got %d, want 0", f.Count(PlaylistHeart))
	}
	if f.Count(PlaylistNote) != 1 {
		t.Fatalf("note count: got %d, want 1", f.Count(PlaylistNote))
	}

	// Note → None (removed)
	kind = mustCycle(t, f, track)
	if kind != "" {
		t.Fatalf("fourth cycle: got %q, want empty", kind)
	}
	if f.Count(PlaylistNote) != 0 {
		t.Fatalf("note count after remove: got %d, want 0", f.Count(PlaylistNote))
	}
	if f.TotalCount() != 0 {
		t.Fatalf("total count: got %d, want 0", f.TotalCount())
	}
}

func TestFavoritesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "favorites.json")
	track1 := filepath.Join(dir, "a.mod")
	track2 := filepath.Join(dir, "b.xm")
	track3 := filepath.Join(dir, "c.it")

	f, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}

	mustCycle(t, f, track1) // → Star
	mustCycle(t, f, track2) // → Star
	mustCycle(t, f, track2) // → Heart
	mustCycle(t, f, track3) // → Star

	// Reload from disk.
	f2, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}

	if f2.Count(PlaylistStar) != 2 {
		t.Fatalf("star count: got %d, want 2", f2.Count(PlaylistStar))
	}
	if f2.Count(PlaylistHeart) != 1 {
		t.Fatalf("heart count: got %d, want 1", f2.Count(PlaylistHeart))
	}
	if f2.Count(PlaylistNote) != 0 {
		t.Fatalf("note count: got %d, want 0", f2.Count(PlaylistNote))
	}
	if f2.GetPlaylist(track1) != PlaylistStar {
		t.Fatalf("track1 playlist: got %q, want %q", f2.GetPlaylist(track1), PlaylistStar)
	}
	if f2.GetPlaylist(track2) != PlaylistHeart {
		t.Fatalf("track2 playlist: got %q, want %q", f2.GetPlaylist(track2), PlaylistHeart)
	}
	if f2.GetPlaylist(track3) != PlaylistStar {
		t.Fatalf("track3 playlist: got %q, want %q", f2.GetPlaylist(track3), PlaylistStar)
	}
}

func TestFavoritesEmptyLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	f, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Writable() {
		t.Fatal("empty favorites should be writable")
	}
	if f.TotalCount() != 0 {
		t.Fatalf("total count: got %d, want 0", f.TotalCount())
	}
}

func TestFavoritesReadOnlyOnCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "favorites.json")
	if err := os.WriteFile(path, []byte("{bad json"), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := LoadFavorites(path)
	if err == nil {
		t.Fatal("expected error for corrupt file")
	}
	if f.Writable() {
		t.Fatal("corrupt favorites should be read-only")
	}
}

func TestFavoritesDuplicateRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "favorites.json")
	track := filepath.Join(dir, "a.mod")

	f, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}

	mustCycle(t, f, track) // → Star
	mustCycle(t, f, track) // → Heart
	mustCycle(t, f, track) // → Note

	if f.GetPlaylist(track) != PlaylistNote {
		t.Fatalf("after 3 cycles: got %q, want %q", f.GetPlaylist(track), PlaylistNote)
	}
	if f.TotalCount() != 1 {
		t.Fatalf("total count: got %d, want 1", f.TotalCount())
	}
}

func TestFavoritesRemove(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "favorites.json")
	track := filepath.Join(dir, "a.mod")

	f, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}

	mustCycle(t, f, track) // → Star
	if f.Count(PlaylistStar) != 1 {
		t.Fatal("expected 1 star track")
	}

	if err := f.Remove(track); err != nil {
		t.Fatal(err)
	}
	if f.Count(PlaylistStar) != 0 {
		t.Fatalf("star count after remove: got %d, want 0", f.Count(PlaylistStar))
	}
	if f.TotalCount() != 0 {
		t.Fatalf("total count: got %d, want 0", f.TotalCount())
	}
}

func TestFavoritesRemoveNonexistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "favorites.json")

	f, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.Remove(filepath.Join(dir, "nonexistent.mod")); err != nil {
		t.Fatal(err)
	}
}

func TestFavoritesSymbol(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "favorites.json")
	track := filepath.Join(dir, "a.mod")

	f, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}

	if f.Symbol(track) != "" {
		t.Fatal("symbol should be empty for unfavourited track")
	}

	mustCycle(t, f, track) // → Star
	if f.Symbol(track) != "★" {
		t.Fatalf("symbol: got %q, want %q", f.Symbol(track), "★")
	}

	mustCycle(t, f, track) // → Heart
	if f.Symbol(track) != "♥" {
		t.Fatalf("symbol: got %q, want %q", f.Symbol(track), "♥")
	}

	mustCycle(t, f, track) // → Note
	if f.Symbol(track) != "♪" {
		t.Fatalf("symbol: got %q, want %q", f.Symbol(track), "♪")
	}

	mustCycle(t, f, track) // → None
	if f.Symbol(track) != "" {
		t.Fatalf("symbol after remove: got %q, want empty", f.Symbol(track))
	}
}

func TestFavoritesTracksReturnsCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "favorites.json")
	track := filepath.Join(dir, "a.mod")

	f, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}
	mustCycle(t, f, track) // → Star

	tracks := f.Tracks(PlaylistStar)
	tracks[0] = "tampered"

	original := f.Tracks(PlaylistStar)
	if len(original) != 1 || original[0] != track {
		t.Fatalf("Tracks() did not return a copy: %v", original)
	}
}

func TestFavoritesVirtualPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "favorites.json")
	virtualTrack := "modland:http://modland.textfiles.com/mods/a/test.mod"

	f, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}

	kind := mustCycle(t, f, virtualTrack)
	if kind != PlaylistStar {
		t.Fatalf("virtual cycle: got %q, want %q", kind, PlaylistStar)
	}

	f2, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}
	if f2.GetPlaylist(virtualTrack) != PlaylistStar {
		t.Fatal("virtual track not persisted")
	}
}

func TestFavoritesTrackTitle(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/music/test.mod", "test.mod"},
		{"modland:http://example.com/mods/author/format/file.xm", "file.xm"},
		{"modarchive:http://example.com/files/a/b/file.zip#nested.mod", "nested.mod"},
	}
	for _, tt := range tests {
		got := FavoriteTrackTitle(tt.path)
		if got != tt.want {
			t.Errorf("FavoriteTrackTitle(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestFavoritesPreservesOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "favorites.json")
	tracks := make([]string, 5)
	for i := range tracks {
		tracks[i] = filepath.Join(dir, string(rune('a'+i))+".mod")
	}

	f, _ := LoadFavorites(path)
	// Add to Star in order: c, a, e, b, d
	mustCycle(t, f, tracks[2]) // c → Star
	mustCycle(t, f, tracks[0]) // a → Star
	mustCycle(t, f, tracks[4]) // e → Star
	mustCycle(t, f, tracks[1]) // b → Star
	mustCycle(t, f, tracks[3]) // d → Star

	got := f.Tracks(PlaylistStar)
	want := []string{tracks[2], tracks[0], tracks[4], tracks[1], tracks[3]}
	if len(got) != len(want) {
		t.Fatalf("len: got %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("order[%d]: got %q, want %q", i, got[i], want[i])
		}
	}

	// Reload and verify order is preserved.
	f2, _ := LoadFavorites(path)
	got2 := f2.Tracks(PlaylistStar)
	for i := range got2 {
		if got2[i] != want[i] {
			t.Fatalf("reload order[%d]: got %q, want %q", i, got2[i], want[i])
		}
	}
}

func TestFilterAvailable(t *testing.T) {
	dir := t.TempDir()
	// Create two files, leave one absent.
	a := filepath.Join(dir, "a.mod")
	b := filepath.Join(dir, "b.xm")
	if err := os.WriteFile(a, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	absent := filepath.Join(dir, "missing.it")
	remote := "modland:http://example.com/song.mod"

	input := []string{a, absent, b, remote}
	got := FilterAvailable(input)
	want := []string{a, b, remote}
	if len(got) != len(want) {
		t.Fatalf("len: got %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFilterAvailableAllMissing(t *testing.T) {
	dir := t.TempDir()
	absent := filepath.Join(dir, "nope.it")
	got := FilterAvailable([]string{absent})
	if len(got) != 0 {
		t.Fatalf("expected empty, got %d", len(got))
	}
}
