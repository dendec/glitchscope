package player

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/dendec/glitchscope/internal/openmpt"
	"github.com/dendec/glitchscope/internal/soloud"
	"github.com/dendec/glitchscope/internal/xmp"
)

type TrackMeta struct {
	Duration float64 `json:"d"`
	BPM      float64 `json:"b,omitempty"`
	Channels int     `json:"c,omitempty"`
	Comment  string  `json:"m,omitempty"` // tracker message/comment

	// Audio file tags (MP3/FLAC/Ogg/Opus via FFmpeg).
	Title       string `json:"ti,omitempty"`
	Artist      string `json:"ar,omitempty"`
	Album       string `json:"al,omitempty"`
	AlbumArtist string `json:"aa,omitempty"`
	Genre       string `json:"ge,omitempty"`
	Date        string `json:"da,omitempty"`
	Track       string `json:"tr,omitempty"` // track number string
	Composer    string `json:"co,omitempty"`
	Disc        string `json:"di,omitempty"` // disc number string

	// Extra tags not covered by the named fields above.
	Extra map[string]string `json:"ex,omitempty"`
}

type albumMeta struct {
	Version int                  `json:"v"`
	Tracks  map[string]TrackMeta `json:"t"`
}

const (
	metaFileName = ".gsa_meta.json"
	metaVersion  = 3
)

// readMetaCache reads the metadata cache for an album directory.
func readMetaCache(albumPath string) *albumMeta {
	fpath := filepath.Join(albumPath, metaFileName)
	f, err := os.Open(fpath)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var m albumMeta
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		return nil
	}
	if m.Version != metaVersion || m.Tracks == nil {
		return nil
	}
	return &m
}

// writeMetaCache writes the metadata cache for an album directory.
func writeMetaCache(albumPath string, m *albumMeta) error {
	m.Version = metaVersion
	fpath := filepath.Join(albumPath, metaFileName)
	f, err := os.Create(fpath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "")
	return enc.Encode(m)
}

// id3v1Genres maps ID3v1 genre numbers to human-readable names.
var id3v1Genres = [192]string{
	"Blues", "Classic Rock", "Country", "Dance", "Disco", "Funk",
	"Grunge", "Hip-hop", "Jazz", "Metal", "New age", "Oldies",
	"Other", "Pop", "Rhythm and blues", "Rap", "Reggae", "Rock",
	"Techno", "Industrial", "Alternative", "Ska", "Death metal",
	"Pranks", "Soundtrack", "Euro-techno", "Ambient", "Trip-hop",
	"Vocal", "Jazz & funk", "Fusion", "Trance", "Classical",
	"Instrumental", "Acid", "House", "Game", "Sound clip", "Gospel",
	"Noise", "Alternative rock", "Bass", "Soul", "Punk", "Space",
	"Meditative", "Instrumental pop", "Instrumental rock", "Ethnic",
	"Gothic", "Darkwave", "Techno-industrial", "Electronic", "Pop-folk",
	"Eurodance", "Dream", "Southern rock", "Comedy", "Cult", "Gangsta",
	"Top 40", "Christian rap", "Pop/funk", "Jungle music", "Native US",
	"Cabaret", "New wave", "Psychedelic", "Rave", "Showtunes",
	"Trailer", "Lo-fi", "Tribal", "Acid punk", "Acid jazz", "Polka",
	"Retro", "Musical", "Rock 'n' roll", "Hard rock", "Folk",
	"Folk rock", "National folk", "Swing", "Fast fusion", "Bebop",
	"Latin", "Revival", "Celtic", "Bluegrass", "Avantgarde",
	"Gothic rock", "Progressive rock", "Psychedelic rock",
	"Symphonic rock", "Slow rock", "Big band", "Chorus",
	"Easy listening", "Acoustic", "Humour", "Speech", "Chanson",
	"Opera", "Chamber music", "Sonata", "Symphony", "Booty bass",
	"Primus", "Porn groove", "Satire", "Slow jam", "Club", "Tango",
	"Samba", "Folklore", "Ballad", "Power ballad", "Rhythmic Soul",
	"Freestyle", "Duet", "Punk rock", "Drum solo", "A cappella",
	"Euro-house", "Dance hall", "Goa music", "Drum & bass",
	"Club-house", "Hardcore techno", "Terror", "Indie", "Britpop",
	"Negerpunk", "Polsk punk", "Beat", "Christian gangsta rap",
	"Heavy metal", "Black metal", "Crossover", "Contemporary Christian",
	"Christian rock", "Merengue", "Salsa", "Thrash metal", "Anime",
	"Jpop", "Synthpop", "Christmas", "Art rock", "Baroque",
	"Bhangra", "Big beat", "Breakbeat", "Chillout", "Downtempo",
	"Dub", "EBM", "Eclectic", "Electro", "Electroclash", "Emo",
	"Experimental", "Garage", "Global", "IDM", "Illbient",
	"Industro-Goth", "Jam band", "Krautrock", "Leftfield", "Lounge",
	"Math rock", "New romantic", "Nu-breakz", "Post-punk", "Post-rock",
	"Psytrance", "Shoegaze", "Space rock", "Trop rock", "World music",
	"Neoclassical", "Audiobook", "Audio theatre", "Neue Deutsche Welle",
	"Podcast", "Indie rock", "G-Funk", "Dubstep", "Garage rock",
	"Psybient",
}

// resolveGenre converts a raw genre tag to a human-readable name.
// Numeric ID3v1 genre codes (0–191) are resolved to their names;
// everything else is returned as-is.
func resolveGenre(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return s
		}
		n = n*10 + int(c-'0')
		if n >= len(id3v1Genres) {
			return s
		}
	}
	return id3v1Genres[n]
}

// extractMetaFromFile extracts track metadata (duration, BPM, channels,
// comment) from a local file. Pure function — no caching, no side effects —
// so it can be reused for on-demand fills and comment refreshes alike.
func extractMetaFromFile(path string) TrackMeta {
	ext := strings.ToLower(filepath.Ext(path))
	m := TrackMeta{}
	if isTrackerExt(ext) {
		if bpm, ch, dur, err := xmp.GetTrackerMeta(path); err == nil {
			m.Duration = dur
			m.BPM = bpm
			m.Channels = ch
		} else if openmpt.HasExt(ext) {
			if fileBuf, err := os.ReadFile(path); err == nil {
				if bpm, ch, dur, err := openmpt.GetTrackerMeta(fileBuf); err == nil {
					m.Duration = dur
					m.BPM = bpm
					m.Channels = ch
				}
			}
		}
		// Tracker message/comment (XM/IT/MOD/S3M text).
		if fileBuf, err := os.ReadFile(path); err == nil {
			if msg := openmpt.GetMessage(fileBuf); msg != "" {
				m.Comment = msg
			}
		}
	} else if isFfmpegExt(ext) {
		if fileBuf, err := os.ReadFile(path); err == nil {
			if source, err := soloud.NewFfmpeg(fileBuf); err == nil {
				m.Duration = source.GetLength()
				m.Channels = source.GetChannels()
				source.Destroy()
			}
		}
		// Read audio tags (title, artist, etc.) via FFmpeg.
		if tags := soloud.FfmpegReadTags(path); tags != nil {
			m.Title = tags["title"]
			m.Artist = tags["artist"]
			m.Album = tags["album"]
			m.AlbumArtist = tags["album_artist"]
			m.Genre = resolveGenre(tags["genre"])
			m.Date = tags["date"]
			m.Track = tags["track"]
			m.Composer = tags["composer"]
			m.Disc = tags["disc"]
			if m.Comment == "" {
				m.Comment = tags["comment"]
			}
			// Collect remaining tags as extra info.
			known := map[string]bool{
				"title": true, "artist": true, "album": true,
				"album_artist": true, "genre": true, "date": true,
				"track": true, "composer": true, "disc": true,
				"comment": true,
				// Container-level noise, not useful for display.
				"major_brand": true, "minor_version": true,
				"compatible_brands": true,
			}
			for k, v := range tags {
				if known[k] || v == "" {
					continue
				}
				if m.Extra == nil {
					m.Extra = make(map[string]string)
				}
				m.Extra[k] = v
			}
		}
	} else if w, err := soloud.LoadWav(path); err == nil {
		m.Duration = w.GetLength()
		w.Destroy()
	}
	return m
}

// ExtractCoverArt extracts embedded cover art from an audio file.
// Returns raw image bytes (typically JPEG) or nil if absent.
func ExtractCoverArt(path string) []byte {
	return soloud.FfmpegReadCoverArt(path)
}

// ReadFileMeta reads track metadata from a local audio file. Used by the
// NC info panel to display tag info for the selected file.
func ReadFileMeta(path string) TrackMeta {
	return extractMetaFromFile(path)
}
