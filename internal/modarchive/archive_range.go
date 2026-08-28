package modarchive

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/dendec/glitchscope/internal/formats"
	"github.com/dendec/glitchscope/internal/player"
	"github.com/dendec/glitchscope/internal/util"
)

const (
	archiveTailBytes = 128 << 10
	zipLocalHeader   = 0x04034b50
	zipCentralHeader = 0x02014b50
	zipEndHeader     = 0x06054b50
)

type archiveRecord struct {
	name             string
	offset           int64
	endOffset        int64
	compressedSize   uint64
	uncompressedSize uint64
	crc32            uint32
	compression      uint16
	flags            uint16
}

type rangeProgressReader struct {
	reader     io.Reader
	read       int64
	total      int64
	onProgress func(int64, int64)
}

func (r *rangeProgressReader) Read(buffer []byte) (int, error) {
	read, err := r.reader.Read(buffer)
	if read > 0 {
		r.read += int64(read)
		r.onProgress(r.read, r.total)
	}
	return read, err
}

func isSnapshotDirectoryURL(targetURL *url.URL) bool {
	return strings.HasPrefix(strings.TrimPrefix(targetURL.Path, "/"), SnapshotDir+"/")
}

// IsSnapshotArchiveURL reports whether targetURL names a bucket ZIP in the official snapshot.
func IsSnapshotArchiveURL(targetURL string) bool {
	u, err := url.Parse(targetURL)
	if err != nil {
		return false
	}
	return isSnapshotDirectoryURL(u) && strings.EqualFold(path.Ext(u.Path), ".zip")
}

func archiveEntryURL(archiveURL, entryName string) (string, error) {
	u, err := url.Parse(archiveURL)
	if err != nil {
		return "", err
	}
	u.Fragment = entryName
	return u.String(), nil
}

// AlbumURL returns the directory or snapshot archive containing a track URL.
func AlbumURL(trackURL string) string {
	u, err := url.Parse(trackURL)
	if err == nil && u.Fragment != "" {
		u.Fragment = ""
		return u.String()
	}
	slash := strings.LastIndexByte(trackURL, '/')
	if slash < 0 {
		return ""
	}
	return trackURL[:slash+1]
}

func fetchAndCacheArchiveIndex(ctx context.Context, baseDir, archiveURL string) ([]DirItem, error) {
	indexDir, err := IndexDir(baseDir)
	if err != nil {
		return nil, err
	}
	cacheFile := filepath.Join(indexDir, urlHash(archiveURL)+".json")

	items, err := fetchArchiveIndex(ctx, archiveURL)
	if err != nil {
		if cached, cacheErr := loadCachedIndex(cacheFile); cacheErr == nil && len(cached) > 0 {
			return cached, nil
		}
		return nil, err
	}

	saveCachedIndex(cacheFile, items)
	memCacheMu.Lock()
	memCache[archiveURL] = items
	memCacheMu.Unlock()
	return items, nil
}

func fetchArchiveIndex(ctx context.Context, archiveURL string) ([]DirItem, error) {
	tail, tailStart, total, err := fetchSuffixRange(ctx, archiveURL, archiveTailBytes, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch archive index %s: %w", archiveURL, err)
	}

	centralOffset, centralSize, records, err := parseEndRecord(tail, total)
	if err != nil {
		return nil, fmt.Errorf("parse archive end %s: %w", archiveURL, err)
	}
	centralEnd := centralOffset + centralSize
	var central []byte
	if centralOffset >= tailStart && centralEnd <= total {
		start := centralOffset - tailStart
		central = tail[start : start+centralSize]
	} else {
		central, _, err = fetchByteRange(ctx, archiveURL, centralOffset, centralEnd-1, nil)
		if err != nil {
			return nil, fmt.Errorf("fetch archive central directory %s: %w", archiveURL, err)
		}
	}

	parsed, err := parseCentralDirectory(central, records, centralOffset)
	if err != nil {
		return nil, fmt.Errorf("parse archive directory %s: %w", archiveURL, err)
	}

	items := make([]DirItem, 0, len(parsed))
	for _, record := range parsed {
		cleanName, ok := supportedArchiveEntry(record.name)
		if !ok || record.flags&1 != 0 {
			continue
		}
		entryURL, err := archiveEntryURL(archiveURL, record.name)
		if err != nil {
			return nil, fmt.Errorf("archive entry URL: %w", err)
		}
		items = append(items, DirItem{
			Name:             record.name,
			URL:              entryURL,
			Kind:             KindFile,
			Size:             int64(record.uncompressedSize),
			CleanName:        cleanName,
			ArchiveOffset:    record.offset,
			ArchiveEndOffset: record.endOffset,
			CompressedSize:   record.compressedSize,
			CRC32:            record.crc32,
			Compression:      record.compression,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].CleanName) < strings.ToLower(items[j].CleanName)
	})
	return items, nil
}

func parseEndRecord(tail []byte, total int64) (centralOffset, centralSize int64, records uint16, err error) {
	for offset := len(tail) - 22; offset >= 0; offset-- {
		if binary.LittleEndian.Uint32(tail[offset:]) != zipEndHeader {
			continue
		}
		commentLen := int(binary.LittleEndian.Uint16(tail[offset+20:]))
		if offset+22+commentLen != len(tail) {
			continue
		}
		if binary.LittleEndian.Uint16(tail[offset+4:]) != 0 || binary.LittleEndian.Uint16(tail[offset+6:]) != 0 {
			return 0, 0, 0, errors.New("multi-disk ZIP is unsupported")
		}
		records = binary.LittleEndian.Uint16(tail[offset+10:])
		if records == 0xffff {
			return 0, 0, 0, errors.New("ZIP64 is unsupported")
		}
		centralSize = int64(binary.LittleEndian.Uint32(tail[offset+12:]))
		centralOffset = int64(binary.LittleEndian.Uint32(tail[offset+16:]))
		if centralOffset < 0 || centralSize < 0 || centralOffset+centralSize > total {
			return 0, 0, 0, errors.New("invalid central directory bounds")
		}
		return centralOffset, centralSize, records, nil
	}
	return 0, 0, 0, errors.New("ZIP end record not found")
}

func parseCentralDirectory(data []byte, count uint16, centralOffset int64) ([]archiveRecord, error) {
	records := make([]archiveRecord, 0, count)
	offset := 0
	for range count {
		if len(data)-offset < 46 || binary.LittleEndian.Uint32(data[offset:]) != zipCentralHeader {
			return nil, errors.New("invalid central directory entry")
		}
		nameLen := int(binary.LittleEndian.Uint16(data[offset+28:]))
		extraLen := int(binary.LittleEndian.Uint16(data[offset+30:]))
		commentLen := int(binary.LittleEndian.Uint16(data[offset+32:]))
		entryEnd := offset + 46 + nameLen + extraLen + commentLen
		if entryEnd > len(data) {
			return nil, errors.New("truncated central directory entry")
		}
		nameBytes := data[offset+46 : offset+46+nameLen]
		if !utf8.Valid(nameBytes) {
			return nil, errors.New("non-UTF-8 ZIP entry name is unsupported")
		}
		records = append(records, archiveRecord{
			name:             string(nameBytes),
			offset:           int64(binary.LittleEndian.Uint32(data[offset+42:])),
			compressedSize:   uint64(binary.LittleEndian.Uint32(data[offset+20:])),
			uncompressedSize: uint64(binary.LittleEndian.Uint32(data[offset+24:])),
			crc32:            binary.LittleEndian.Uint32(data[offset+16:]),
			compression:      binary.LittleEndian.Uint16(data[offset+10:]),
			flags:            binary.LittleEndian.Uint16(data[offset+8:]),
		})
		offset = entryEnd
	}

	sort.Slice(records, func(i, j int) bool { return records[i].offset < records[j].offset })
	for i := range records {
		if records[i].offset < 0 || records[i].offset >= centralOffset {
			return nil, errors.New("invalid local header offset")
		}
		records[i].endOffset = centralOffset
		if i+1 < len(records) {
			records[i].endOffset = records[i+1].offset
		}
		if records[i].endOffset <= records[i].offset {
			return nil, errors.New("overlapping ZIP entries")
		}
	}
	return records, nil
}

func supportedArchiveEntry(name string) (string, bool) {
	if strings.HasSuffix(name, "/") {
		return "", false
	}
	base := path.Base(name)
	ext := strings.ToLower(path.Ext(base))
	if formats.IsSupportedExt(ext) {
		return base, true
	}
	if ext == ".zip" {
		inner := strings.TrimSuffix(base, path.Ext(base))
		if formats.IsSupportedExt(path.Ext(inner)) {
			return inner, true
		}
	}
	return "", false
}

func fetchSuffixRange(ctx context.Context, targetURL string, size int64, onProgress func(int64, int64)) ([]byte, int64, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, 0, 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=-%d", size))
	data, start, total, err := doRangeRequest(req, onProgress)
	return data, start, total, err
}

func fetchByteRange(ctx context.Context, targetURL string, start, end int64, onProgress func(int64, int64)) ([]byte, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	data, actualStart, total, err := doRangeRequest(req, onProgress)
	if err != nil {
		return nil, 0, err
	}
	if actualStart != start || int64(len(data)) != end-start+1 {
		return nil, 0, errors.New("server returned an unexpected byte range")
	}
	return data, total, nil
}

func doRangeRequest(req *http.Request, onProgress func(int64, int64)) ([]byte, int64, int64, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return nil, 0, 0, fmt.Errorf("range request: HTTP %d", resp.StatusCode)
	}

	var start, end, total int64
	if _, err := fmt.Sscanf(resp.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &total); err != nil {
		return nil, 0, 0, fmt.Errorf("invalid Content-Range: %w", err)
	}
	reader := io.Reader(resp.Body)
	if onProgress != nil {
		reader = &rangeProgressReader{reader: reader, total: end - start + 1, onProgress: onProgress}
	}
	data, err := io.ReadAll(io.LimitReader(reader, end-start+2))
	if err != nil {
		return nil, 0, 0, err
	}
	if int64(len(data)) != end-start+1 {
		return nil, 0, 0, errors.New("truncated range response")
	}
	return data, start, total, nil
}

func downloadArchiveEntry(ctx context.Context, baseDir, entryURL string, onProgress func(int64, int64)) (string, error) {
	u, err := url.Parse(entryURL)
	if err != nil || u.Fragment == "" {
		return "", fmt.Errorf("invalid archive entry URL %q", entryURL)
	}
	entryName := u.Fragment
	u.Fragment = ""
	archiveURL := u.String()

	items, ok := FetchDirectoryCached(baseDir, archiveURL)
	if !ok {
		items, err = fetchAndCacheArchiveIndex(ctx, baseDir, archiveURL)
		if err != nil {
			return "", err
		}
	}
	var item *DirItem
	for i := range items {
		if items[i].Name == entryName {
			item = &items[i]
			break
		}
	}
	if item == nil {
		return "", fmt.Errorf("archive entry %q not found", entryName)
	}
	if item.ArchiveOffset < 0 || item.ArchiveEndOffset <= item.ArchiveOffset || item.CompressedSize > uint64(item.ArchiveEndOffset-item.ArchiveOffset) {
		return "", errors.New("invalid cached archive entry bounds")
	}

	targetPath := player.ModArchiveCachePath(baseDir, entryURL)
	if targetPath == "" {
		return "", errors.New("invalid archive entry cache path")
	}
	if info, err := os.Stat(targetPath); err == nil && info.Size() > 0 {
		return targetPath, nil
	}

	block, _, err := fetchByteRange(ctx, archiveURL, item.ArchiveOffset, item.ArchiveEndOffset-1, onProgress)
	if err != nil {
		return "", fmt.Errorf("download archive entry %s: %w", entryURL, err)
	}
	return extractArchiveBlock(block, *item, targetPath)
}

func extractArchiveBlock(block []byte, item DirItem, targetPath string) (string, error) {
	if len(block) < 30 || binary.LittleEndian.Uint32(block) != zipLocalHeader {
		return "", errors.New("invalid ZIP local header")
	}
	nameLen := int(binary.LittleEndian.Uint16(block[26:]))
	extraLen := int(binary.LittleEndian.Uint16(block[28:]))
	dataOffset := 30 + nameLen + extraLen
	if dataOffset > len(block) || item.CompressedSize > uint64(len(block)-dataOffset) {
		return "", errors.New("truncated ZIP entry data")
	}
	if string(block[30:30+nameLen]) != item.Name {
		return "", errors.New("ZIP local and central names differ")
	}
	compressed := bytes.NewReader(block[dataOffset : dataOffset+int(item.CompressedSize)])
	var reader io.ReadCloser
	switch item.Compression {
	case 0:
		reader = io.NopCloser(compressed)
	case 8:
		reader = flate.NewReader(compressed)
	default:
		return "", fmt.Errorf("unsupported ZIP compression method %d", item.Compression)
	}
	defer reader.Close()

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(targetPath), "range*.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	hash := crc32.NewIEEE()
	written, copyErr := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(reader, item.Size+1))
	closeErr := tmp.Close()
	if copyErr != nil {
		return "", fmt.Errorf("decompress archive entry: %w", copyErr)
	}
	if closeErr != nil {
		return "", closeErr
	}
	if uint64(written) != uint64(item.Size) || hash.Sum32() != item.CRC32 {
		return "", errors.New("archive entry size or CRC mismatch")
	}

	if strings.EqualFold(path.Ext(item.Name), ".zip") {
		return util.ExtractModuleFromZip(tmpPath, targetPath, formats.IsSupportedExt)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return "", fmt.Errorf("cache archive entry: %w", err)
	}
	return targetPath, nil
}
