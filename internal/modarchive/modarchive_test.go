package modarchive

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestFormatDirName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"modarchive_2008_additions", "2008"},
		{"modarchive_2023_additions", "2023"},
		{SnapshotDir, SnapshotLabel},
		{AddendumDir, AddendumLabel},
		{"MOD", "MOD"},
		{"XM", "XM"},
		{"", "ModArchive"},
	}

	for _, tt := range tests {
		got := FormatDirName(tt.input)
		if got != tt.expected {
			t.Errorf("FormatDirName(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}

func TestParseDirectoryListing_Root(t *testing.T) {
	htmlBody := `
<!DOCTYPE html>
<html>
<body>
<table>
<tr><td><a href="modarchive_2007_official_snapshot_120000_modules">modarchive_2007_official_snapshot</a></td></tr>
<tr><td><a href="modarchive_2007_official_snapshot_addendum1">modarchive_2007_official_snapshot_addendum1</a></td></tr>
<tr><td><a href="modarchive_2008_additions">modarchive_2008_additions</a></td></tr>
<tr><td><a href="modarchive_2023_additions">modarchive_2023_additions</a></td></tr>
<tr><td><a href="kiarchive.zip">kiarchive.zip</a></td></tr>
</table>
</body>
</html>
`
	items, err := ParseDirectoryListing(htmlBody, "http://modarchive.textfiles.com/")
	if err != nil {
		t.Fatalf("ParseDirectoryListing root failed: %v", err)
	}

	if len(items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(items))
	}

	if items[0].CleanName != SnapshotLabel || items[0].Kind != KindDir {
		t.Errorf("unexpected item 0: %+v", items[0])
	}
	if items[1].CleanName != AddendumLabel || items[1].Kind != KindDir {
		t.Errorf("unexpected item 1: %+v", items[1])
	}
	if items[2].CleanName != "2008" || items[2].Kind != KindDir {
		t.Errorf("unexpected item 2: %+v", items[2])
	}
	if items[3].CleanName != "2023" || items[3].Kind != KindDir {
		t.Errorf("unexpected item 3: %+v", items[3])
	}
}

func TestParseDirectoryListing_Subfolder(t *testing.T) {
	htmlBody := `
<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 3.2 Final//EN">
<html>
 <body>
<h1>Index of /modarchive_2008_additions/MOD/A</h1>
<ul><li><a href="/modarchive_2008_additions/MOD/"> Parent Directory</a></li>
<li><a href="a_little_bit_o_fun.mod.zip"> a_little_bit_o_fun.mod.zip</a></li>
<li><a href="amazing_amiga.mod.zip"> amazing_amiga.mod.zip</a></li>
</ul>
</body></html>
`
	items, err := ParseDirectoryListing(htmlBody, "http://modarchive.textfiles.com/modarchive_2008_additions/MOD/A/")
	if err != nil {
		t.Fatalf("ParseDirectoryListing subfolder failed: %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	if items[0].CleanName != "a_little_bit_o_fun.mod" || items[0].Kind != KindFile {
		t.Errorf("unexpected item 0: %+v", items[0])
	}
	if items[1].CleanName != "amazing_amiga.mod" || items[1].Kind != KindFile {
		t.Errorf("unexpected item 1: %+v", items[1])
	}
}

func TestParseDirectoryListing_HvlZip(t *testing.T) {
	htmlBody := `<a href="street_fighter_ii_-_vega.hvl.zip"> street_fighter_ii_-_vega.hvl.zip</a>`

	items, err := ParseDirectoryListing(htmlBody, "http://modarchive.textfiles.com/modarchive_2023_additions/HVL/S/")
	if err != nil {
		t.Fatalf("ParseDirectoryListing HVL failed: %v", err)
	}

	if len(items) != 1 || items[0].CleanName != "street_fighter_ii_-_vega.hvl" || items[0].Kind != KindFile {
		t.Fatalf("unexpected HVL item: %+v", items)
	}
}

func TestParseDirectoryListing_SnapshotArchive(t *testing.T) {
	htmlBody := `<a href="A0.zip">A0.zip</a><a href="AA.zip">AA.zip</a>`
	targetURL := BaseURL + SnapshotDir + "/A/"

	items, err := ParseDirectoryListing(htmlBody, targetURL)
	if err != nil {
		t.Fatalf("ParseDirectoryListing snapshot failed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("snapshot items = %d, want 2", len(items))
	}
	if items[0].Kind != KindArchive || items[0].CleanName != "A0" {
		t.Fatalf("snapshot item = %+v, want archive A0", items[0])
	}
}

func TestParseDirectoryListing_AddendumArchive(t *testing.T) {
	htmlBody := `<a href="A-.zip">A-.zip</a>`
	targetURL := BaseURL + AddendumDir + "/"

	items, err := ParseDirectoryListing(htmlBody, targetURL)
	if err != nil {
		t.Fatalf("ParseDirectoryListing addendum failed: %v", err)
	}
	if len(items) != 1 || items[0].Kind != KindArchive || items[0].CleanName != "A-" {
		t.Fatalf("addendum items = %+v, want archive A-", items)
	}
}

func TestSnapshotArchiveRangeIndexAndExtract(t *testing.T) {
	innerBuffer := new(bytes.Buffer)
	innerWriter := zip.NewWriter(innerBuffer)
	innerFile, err := innerWriter.Create("a0d_agep.xm")
	if err != nil {
		t.Fatal(err)
	}
	xmContent := []byte("Extended Module: range test")
	if _, err := innerFile.Write(xmContent); err != nil {
		t.Fatal(err)
	}
	if err := innerWriter.Close(); err != nil {
		t.Fatal(err)
	}

	outerBuffer := new(bytes.Buffer)
	outerWriter := zip.NewWriter(outerBuffer)
	directFile, err := outerWriter.Create("a0v_tune.mod")
	if err != nil {
		t.Fatal(err)
	}
	modContent := []byte("M.K. direct range test")
	if _, err := directFile.Write(modContent); err != nil {
		t.Fatal(err)
	}
	innerHeader := &zip.FileHeader{Name: "a0d_agep.xm.zip", Method: zip.Store}
	innerEntry, err := outerWriter.CreateHeader(innerHeader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := innerEntry.Write(innerBuffer.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := outerWriter.Close(); err != nil {
		t.Fatal(err)
	}
	outerData := outerBuffer.Bytes()

	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		rangeHeader := request.Header.Get("Range")
		ranges = append(ranges, rangeHeader)
		start, end, parseErr := testRangeBounds(rangeHeader, int64(len(outerData)))
		if parseErr != nil {
			http.Error(writer, parseErr.Error(), http.StatusRequestedRangeNotSatisfiable)
			return
		}
		writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(outerData)))
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = writer.Write(outerData[start : end+1])
	}))
	defer server.Close()

	archiveURL := server.URL + "/" + SnapshotDir + "/A/A0.zip"
	baseDir := t.TempDir()
	items, err := FetchDirectory(baseDir, archiveURL)
	if err != nil {
		t.Fatalf("FetchDirectory archive: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("archive items = %d, want 2", len(items))
	}
	if len(ranges) != 1 || ranges[0] != "bytes=-131072" {
		t.Fatalf("index ranges = %q, want one suffix request", ranges)
	}

	byName := make(map[string]DirItem, len(items))
	for _, item := range items {
		byName[item.Name] = item
	}
	directPath, err := DownloadAndExtract(context.Background(), baseDir, byName["a0v_tune.mod"].URL, nil)
	if err != nil {
		t.Fatalf("download direct entry: %v", err)
	}
	directContent, err := os.ReadFile(directPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(directContent, modContent) {
		t.Fatalf("direct content = %q, want %q", directContent, modContent)
	}

	nestedPath, err := DownloadAndExtract(context.Background(), baseDir, byName["a0d_agep.xm.zip"].URL, nil)
	if err != nil {
		t.Fatalf("download nested entry: %v", err)
	}
	nestedContent, err := os.ReadFile(nestedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(nestedContent, xmContent) {
		t.Fatalf("nested content = %q, want %q", nestedContent, xmContent)
	}
	if len(ranges) != 3 {
		t.Fatalf("requests = %q, want index plus one range per selected track", ranges)
	}
}

func TestSnapshotArchiveRejectsFullResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("full archive must not be accepted"))
	}))
	defer server.Close()

	archiveURL := server.URL + "/" + SnapshotDir + "/A/A0.zip"
	if _, err := FetchDirectory(t.TempDir(), archiveURL); err == nil || !strings.Contains(err.Error(), "HTTP 200") {
		t.Fatalf("FetchDirectory error = %v, want rejected non-range response", err)
	}
}

func TestFetchSuffixRangeRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Range", "bytes 0-9/10")
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = writer.Write([]byte("0123456789"))
	}))
	defer server.Close()

	if _, _, _, err := fetchSuffixRange(context.Background(), server.URL, 4, nil); err == nil || !strings.Contains(err.Error(), "requested at most 4") {
		t.Fatalf("fetchSuffixRange error = %v, want oversized response rejection", err)
	}
}

func TestFetchSuffixRangeRejectsWrongOffset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Range", "bytes 2-5/10")
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = writer.Write([]byte("2345"))
	}))
	defer server.Close()

	if _, _, _, err := fetchSuffixRange(context.Background(), server.URL, 4, nil); err == nil || !strings.Contains(err.Error(), "unexpected suffix range") {
		t.Fatalf("fetchSuffixRange error = %v, want wrong offset rejection", err)
	}
}

func TestParseContentRangeRejectsInvalidBounds(t *testing.T) {
	for _, header := range []string{
		"items 0-9/10",
		"bytes 9-0/10",
		"bytes 0-10/10",
		"bytes 0-9/*",
		"bytes 0-9/10 trailing",
	} {
		if _, _, _, err := parseContentRange(header); err == nil {
			t.Errorf("parseContentRange(%q) succeeded", header)
		}
	}
}

func TestParseCentralDirectoryDecodesLegacyCP437Name(t *testing.T) {
	name := []byte{'t', 0x84, 's', 't', '.', 'm', 'o', 'd'}
	central := testCentralDirectoryEntry(name, 0)

	records, err := parseCentralDirectory(central, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if records[0].name != "täst.mod" {
		t.Fatalf("name = %q, want %q", records[0].name, "täst.mod")
	}
}

func TestParseCentralDirectoryRejectsInvalidFlaggedUTF8Name(t *testing.T) {
	central := testCentralDirectoryEntry([]byte{'t', 0x84, '.', 'm', 'o', 'd'}, 0x800)

	if _, err := parseCentralDirectory(central, 1, 100); err == nil || !strings.Contains(err.Error(), "invalid UTF-8") {
		t.Fatalf("parseCentralDirectory error = %v, want invalid UTF-8", err)
	}
}

func TestExtractArchiveBlockDecodesLegacyCP437Name(t *testing.T) {
	rawName := []byte{'t', 0x84, 's', 't', '.', 'm', 'o', 'd'}
	content := []byte("legacy module")
	block := make([]byte, 30+len(rawName)+len(content))
	binary.LittleEndian.PutUint32(block, zipLocalHeader)
	binary.LittleEndian.PutUint16(block[26:], uint16(len(rawName)))
	copy(block[30:], rawName)
	copy(block[30+len(rawName):], content)
	item := DirItem{
		Name:           "täst.mod",
		Size:           int64(len(content)),
		CompressedSize: uint64(len(content)),
		CRC32:          crc32.ChecksumIEEE(content),
	}
	targetPath := filepath.Join(t.TempDir(), "täst.mod")

	path, err := extractArchiveBlock(block, item, targetPath)
	if err != nil {
		t.Fatal(err)
	}
	extracted, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(extracted, content) {
		t.Fatalf("content = %q, want %q", extracted, content)
	}
}

func testCentralDirectoryEntry(name []byte, flags uint16) []byte {
	central := make([]byte, 46+len(name))
	binary.LittleEndian.PutUint32(central, zipCentralHeader)
	binary.LittleEndian.PutUint16(central[8:], flags)
	binary.LittleEndian.PutUint16(central[28:], uint16(len(name)))
	copy(central[46:], name)
	return central
}

func testRangeBounds(header string, total int64) (int64, int64, error) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, fmt.Errorf("missing byte range")
	}
	spec := strings.TrimPrefix(header, "bytes=")
	if strings.HasPrefix(spec, "-") {
		suffix, err := strconv.ParseInt(strings.TrimPrefix(spec, "-"), 10, 64)
		if err != nil || suffix <= 0 {
			return 0, 0, fmt.Errorf("invalid suffix range")
		}
		return max(0, total-suffix), total - 1, nil
	}
	var start, end int64
	if _, err := fmt.Sscanf(spec, "%d-%d", &start, &end); err != nil || start < 0 || end < start || end >= total {
		return 0, 0, fmt.Errorf("invalid range %q", spec)
	}
	return start, end, nil
}

func TestDownloadAndExtract_Zip(t *testing.T) {
	// Create mock zip containing test.mod
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	f, err := zw.Create("test.mod")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := f.Write([]byte("M.K. mock module content")); err != nil {
		t.Fatalf("write zip content: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	zipData := buf.Bytes()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(zipData)
	}))
	defer ts.Close()

	tmpDir, err := os.MkdirTemp("", "modarchive_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	remoteURL := ts.URL + "/modarchive_2008_additions/MOD/A/test.mod.zip"
	extractedPath, err := DownloadAndExtract(context.Background(), tmpDir, remoteURL, nil)
	if err != nil {
		t.Fatalf("DownloadAndExtract failed: %v", err)
	}

	content, err := os.ReadFile(extractedPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if string(content) != "M.K. mock module content" {
		t.Errorf("unexpected content: %q", string(content))
	}

	// Verify caching on second call
	cachedPath, err := DownloadAndExtract(context.Background(), tmpDir, remoteURL, nil)
	if err != nil {
		t.Fatalf("DownloadAndExtract cached failed: %v", err)
	}
	if cachedPath != extractedPath {
		t.Errorf("expected cached path %q, got %q", extractedPath, cachedPath)
	}
}

func TestFetchDirectory_Cache(t *testing.T) {
	ResetMemCache()
	defer ResetMemCache()

	fetchCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		html := `<html><body><a href="modarchive_2023_additions">modarchive_2023_additions</a></body></html>`
		_, _ = w.Write([]byte(html))
	}))
	defer ts.Close()

	tmpDir, err := os.MkdirTemp("", "modarchive_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	targetURL := ts.URL + "/"

	// 1. Uncached — synchronous fetch.
	items, err := FetchDirectory(tmpDir, targetURL)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected fetched items, got count=%d, err=%v", len(items), err)
	}
	if items[0].CleanName != "2023" {
		t.Errorf("unexpected item clean name: %s", items[0].CleanName)
	}

	// 2. Second call hits the in-memory cache, no new HTTP request.
	cachedItems, err := FetchDirectory(tmpDir, targetURL)
	if err != nil || len(cachedItems) != 1 {
		t.Fatalf("expected cached result, got count=%d, err=%v", len(cachedItems), err)
	}
	if fetchCount != 1 {
		t.Errorf("expected 1 HTTP GET request due to caching, got %d", fetchCount)
	}
}
