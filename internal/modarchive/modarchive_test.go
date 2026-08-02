package modarchive

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestFormatDirName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"modarchive_2008_additions", "2008"},
		{"modarchive_2023_additions", "2023"},
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

	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	if items[0].CleanName != "2008" || items[0].Kind != KindDir {
		t.Errorf("unexpected item 0: %+v", items[0])
	}
	if items[1].CleanName != "2023" || items[1].Kind != KindDir {
		t.Errorf("unexpected item 1: %+v", items[1])
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
	extractedPath, err := DownloadAndExtract(tmpDir, remoteURL, nil)
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
	cachedPath, err := DownloadAndExtract(tmpDir, remoteURL, nil)
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
