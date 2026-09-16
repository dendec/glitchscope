package player

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
)

const maxMetadataEntries = 2048

type metadataStamp struct {
	Size     int64 `json:"size"`
	Modified int64 `json:"modified"`
}

type metadataEntry struct {
	stamp metadataStamp
	meta  TrackMeta
}

// MetadataReader owns a bounded cache, including successfully read empty tags.
// It is used by one worker at a time; callers must not mutate its resolver.
type MetadataReader struct {
	resolver Resolver
	entries  map[string]metadataEntry
	read     func(string) TrackMeta
}

func NewMetadataReader(baseDir string) *MetadataReader {
	return &MetadataReader{resolver: Resolver{baseDir: baseDir}, entries: make(map[string]metadataEntry), read: extractMetaFromFile}
}

// Load performs local I/O and native decoding. Never call it on the UI thread.
// Album must be an immutable snapshot. Cancellation is checked between files;
// native decoders cannot be interrupted in the middle of a metadata read.
func (r *MetadataReader) Load(ctx context.Context, album Album) []TrackInfo {
	cache := &albumMeta{Tracks: make(map[string]TrackMeta), Files: make(map[string]metadataStamp)}
	if !IsVirtual(album) && album.Path != "" {
		if disk := readMetaCache(album.Path); disk != nil && disk.Files != nil {
			cache = disk
		}
	}
	infos := make([]TrackInfo, len(album.Tracks))
	dirty := false
	for i, path := range album.Tracks {
		if ctx.Err() != nil {
			return nil
		}
		infos[i].Path = path
		local := r.resolver.ResolveLocalPath(path)
		if local == "" || IsRadio(path) {
			continue
		}
		fi, err := os.Stat(local)
		if err != nil {
			if !os.IsNotExist(err) {
				slog.Debug("metadata stat failed", "path", local, "error", err)
			}
			delete(r.entries, local)
			continue
		}
		if !fi.Mode().IsRegular() {
			continue
		}
		stamp := metadataStamp{Size: fi.Size(), Modified: fi.ModTime().UnixNano()}
		entry, found := r.entries[local]
		name := filepath.Base(local)
		if !found || entry.stamp != stamp {
			meta, diskOK := cache.Tracks[name]
			if IsVirtual(album) || !diskOK || cache.Files[name] != stamp {
				meta = r.read(local)
			}
			entry = metadataEntry{stamp: stamp, meta: meta}
			if len(r.entries) >= maxMetadataEntries {
				clear(r.entries)
			}
			r.entries[local] = entry
			cache.Tracks[name], cache.Files[name] = meta, stamp
			dirty = true
		}
		infos[i] = trackInfo(path, fi.Size(), entry.meta)
	}
	if ctx.Err() != nil {
		return nil
	}
	if dirty && !IsVirtual(album) && album.Path != "" {
		if err := writeMetaCache(album.Path, cache); err != nil {
			slog.Warn("write meta cache", "album", album.Name, "error", err)
		}
	}
	return infos
}

func trackInfo(path string, size int64, m TrackMeta) TrackInfo {
	return TrackInfo{
		Path: path, Cached: true, Size: size, Duration: m.Duration, BPM: m.BPM,
		Channels: m.Channels, Comment: m.Comment, Title: m.Title, Artist: m.Artist, Album: m.Album,
		AlbumArtist: m.AlbumArtist, Genre: m.Genre, Date: m.Date, Track: m.Track,
		Composer: m.Composer, Disc: m.Disc, Extra: m.Extra,
	}
}
