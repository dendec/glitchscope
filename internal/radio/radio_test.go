package radio

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseICYTitle(t *testing.T) {
	tests := []struct {
		metadata string
		want     string
	}{
		{"StreamTitle='Artist - Title';", "Artist - Title"},
		{"StreamTitle='A\\'B';", "A'B"},
		{"StreamUrl='x';", ""},
	}
	for _, test := range tests {
		if got := parseICYTitle(test.metadata); got != test.want {
			t.Errorf("parseICYTitle(%q) = %q, want %q", test.metadata, got, test.want)
		}
	}
}

func TestIcyAudioReaderStripsMetadataAndEmitsTitle(t *testing.T) {
	metadata := []byte("StreamTitle='Artist - Track';")
	metadata = append(metadata, bytes.Repeat([]byte{0}, (16-len(metadata)%16)%16)...)
	payload := append([]byte("abcd"), byte(len(metadata)/16))
	payload = append(payload, metadata...)
	payload = append(payload, []byte("efgh")...)

	var titles []string
	reader := &icyAudioReader{
		body:      io.NopCloser(bytes.NewReader(payload)),
		reader:    bufio.NewReader(bytes.NewReader(payload)),
		interval:  4,
		remaining: 4,
		emit: func(title string) {
			titles = append(titles, title)
		},
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "abcdefgh"; got != want {
		t.Fatalf("audio data = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(titles, []string{"Artist - Track"}) {
		t.Fatalf("titles = %#v", titles)
	}
}

func TestOpenStreamUsesOneICYConnection(t *testing.T) {
	metadata := []byte("StreamTitle='One';")
	metadata = append(metadata, bytes.Repeat([]byte{0}, (16-len(metadata)%16)%16)...)
	payload := append([]byte("abcd"), byte(len(metadata)/16))
	payload = append(payload, metadata...)
	payload = append(payload, []byte("efgh")...)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if got := r.Header.Get("Icy-MetaData"); got != "1" {
			t.Errorf("Icy-MetaData = %q", got)
		}
		w.Header().Set("icy-metaint", "4")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	var titles []string
	client := New(t.TempDir())
	t.Cleanup(client.Close)
	stream, err := client.OpenStream(context.Background(), server.URL, func(title string) {
		titles = append(titles, title)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()
	data := make([]byte, 8)
	if _, err := io.ReadFull(stream, data); err != nil {
		t.Fatal(err)
	}
	if string(data) != "abcdefgh" || !reflect.DeepEqual(titles, []string{"One"}) {
		t.Fatalf("data=%q titles=%#v", data, titles)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("HTTP requests = %d, want one", got)
	}
}

func TestParseHLSPlaylist(t *testing.T) {
	contents := "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXT-X-MAP:URI=init.mp4\n#EXTINF:4.0,\nseg-1.m4s\n#EXTINF:4.0,\nhttps://cdn.example/seg-2.m4s\n"
	playlist, err := parseHLSPlaylist("https://radio.example/live/index.m3u8", contents)
	if err != nil {
		t.Fatal(err)
	}
	if playlist.targetDuration != 4*time.Second || playlist.initSegment != "https://radio.example/live/init.mp4" {
		t.Fatalf("playlist metadata = %#v", playlist)
	}
	if len(playlist.segments) != 2 || playlist.segments[0].url != "https://radio.example/live/seg-1.m4s" || playlist.segments[1].url != "https://cdn.example/seg-2.m4s" {
		t.Fatalf("playlist segments = %#v", playlist.segments)
	}
}

func TestParseHLSMasterPlaylist(t *testing.T) {
	contents := "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=128000\nlow/index.m3u8\n"
	playlist, err := parseHLSPlaylist("https://radio.example/master.m3u8", contents)
	if err != nil {
		t.Fatal(err)
	}
	if playlist.variant != "https://radio.example/low/index.m3u8" {
		t.Fatalf("variant = %q", playlist.variant)
	}
}

func TestCleanStationMetadata(t *testing.T) {
	station := cleanStation(Station{
		StationUUID: " id ",
		Name:        "  Radio\n  One  ",
		URL:         " https://example.test/stream ",
		Favicon:     "null",
		Tags:        " rock\tpop ",
	})
	if station.StationUUID != "id" || station.DisplayName() != "Radio One" || station.StreamURL() != "https://example.test/stream" {
		t.Fatalf("clean station = %#v", station)
	}
	if station.Favicon != "" || station.Tags != "rock pop" {
		t.Fatalf("clean metadata = favicon %q, tags %q", station.Favicon, station.Tags)
	}
	unnamed := Station{URL: "https://example.test/fallback", Name: " \n\t "}
	if got := unnamed.DisplayName(); got != unnamed.URL {
		t.Fatalf("unnamed station label = %q", got)
	}
}

func TestParsePlaylists(t *testing.T) {
	dir := t.TempDir()
	m3u := filepath.Join(dir, "stations.m3u")
	if err := os.WriteFile(m3u, []byte("#EXTM3U\n#EXTINF:-1,One\nhttps://one.example/stream\n#EXTINF:-1,Two\nhttps://two.example/aac\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stations, err := ParsePlaylist(m3u)
	if err != nil {
		t.Fatal(err)
	}
	if len(stations) != 2 || stations[0].Name != "One" || stations[1].StreamURL() != "https://two.example/aac" {
		t.Fatalf("M3U stations = %#v", stations)
	}

	pls := filepath.Join(dir, "stations.pls")
	if err := os.WriteFile(pls, []byte("[playlist]\nNumberOfEntries=2\nTitle1=One\nFile1=https://one.example/stream\nFile2=https://two.example/aac\nTitle2=Two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stations, err = ParsePlaylist(pls)
	if err != nil {
		t.Fatal(err)
	}
	if len(stations) != 2 || stations[1].Name != "Two" {
		t.Fatalf("PLS stations = %#v", stations)
	}
}

func TestClientMarkDeadRemovesCachedStation(t *testing.T) {
	dir := t.TempDir()
	client := New(dir)
	t.Cleanup(client.Close)
	station := Station{StationUUID: "dead", Name: "Dead", URL: "https://example/stream", LastCheckOK: 1}
	client.stations[station.StationUUID] = station
	client.queries[queryKey(BrowsePopular, "")] = queryCache{
		Version:  cacheVersion,
		Stations: []Station{station},
	}
	client.MarkDead(station.Path())
	if _, ok := client.Lookup(station.Path()); !ok {
		t.Fatal("failed station metadata must remain available for manual retry and favorites")
	}
	stations, stale := client.Snapshot(BrowsePopular, "")
	if len(stations) != 0 {
		t.Fatalf("dead station remains in query cache: %#v", stations)
	}
	if !stale {
		t.Fatal("dead-station eviction did not invalidate the query cache")
	}
}

func TestClientLoadsCachedFilterValues(t *testing.T) {
	dir := t.TempDir()
	client := New(dir)
	cache := valueCache{Version: cacheVersion, FetchedAt: time.Now(), Values: []string{"rock", "jazz"}}
	client.saveValues(BrowseTag, cache)
	client.Close()

	reloaded := New(dir)
	t.Cleanup(reloaded.Close)
	values, stale := reloaded.Values(BrowseTag)
	if stale || !reflect.DeepEqual(values, []string{"jazz", "rock"}) {
		t.Fatalf("cached values = %#v, stale=%v", values, stale)
	}
}

func TestNormalizeServerURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{name: "hostname", raw: "de1.api.radio-browser.info", want: "https://de1.api.radio-browser.info", ok: true},
		{name: "trailing slash", raw: "https://de1.api.radio-browser.info/", want: "https://de1.api.radio-browser.info", ok: true},
		{name: "http rejected", raw: "http://de1.api.radio-browser.info", ok: false},
		{name: "path rejected", raw: "https://de1.api.radio-browser.info/json", ok: false},
		{name: "foreign host rejected", raw: "https://example.test", ok: false},
		{name: "userinfo rejected", raw: "https://user:password@de1.api.radio-browser.info", ok: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := normalizeServerURL(test.raw)
			if ok != test.ok || got != test.want {
				t.Fatalf("normalizeServerURL(%q) = %q, %v; want %q, %v", test.raw, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestClientRemembersStationAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	station := Station{
		StationUUID: "saved",
		Name:        "Saved Radio",
		URL:         "https://example.test/stream",
	}

	client := New(dir)
	client.AddStations([]Station{station})
	client.RememberStation(station.Path())
	client.Close()

	reloaded := New(dir)
	t.Cleanup(reloaded.Close)
	got, ok := reloaded.Lookup(station.Path())
	if !ok {
		t.Fatal("remembered station was not loaded")
	}
	if got.DisplayName() != station.DisplayName() || got.StreamURL() != station.StreamURL() {
		t.Fatalf("remembered station = %#v, want %#v", got, station)
	}
}

func TestClientRemembersRadioQueueAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	stations := []Station{
		{StationUUID: "first", Name: "First", URL: "https://example.test/first"},
		{StationUUID: "second", Name: "Second", URL: "https://example.test/second"},
	}

	client := New(dir)
	client.AddStations(stations)
	client.RememberQueue(stations[0].Path(), []string{stations[0].Path(), stations[1].Path()})
	client.Close()

	reloaded := New(dir)
	t.Cleanup(reloaded.Close)
	queue := reloaded.LastQueue(stations[0].Path())
	if len(queue) != len(stations) || queue[1].Path() != stations[1].Path() {
		t.Fatalf("remembered queue = %#v, want %#v", queue, stations)
	}
}

func TestBeginDoesNotWaitForSlowServer(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()

	client := New(t.TempDir())
	client.servers = []string{server.URL}
	t.Cleanup(client.Close)

	started := time.Now()
	client.Begin(context.Background(), BrowsePopular, "")
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("Begin blocked for %s on a slow server", elapsed)
	}
	close(release)
}

func TestPollDiscardsStaleRadioResult(t *testing.T) {
	client := New(t.TempDir())
	t.Cleanup(client.Close)
	client.activeID = 2
	client.results <- result{id: 1, kind: BrowsePopular}
	client.results <- result{id: 2, kind: BrowsePopular, err: errors.New("current request failed")}

	kind, _, _, _, err, ok := client.Poll()
	if !ok || kind != BrowsePopular || err == nil {
		t.Fatalf("Poll returned kind=%q err=%v ok=%v, want current error result", kind, err, ok)
	}
}

func TestHLSRedirectAndSequence(t *testing.T) {
	var segments atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start.m3u8":
			http.Redirect(w, r, "/live/index.m3u8", http.StatusFound)
		case "/live/index.m3u8":
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:40\n#EXTINF:1,\nsegment\n#EXTINF:1,\nsegment\n#EXT-X-ENDLIST\n")
		case "/live/segment":
			segments.Add(1)
			_, _ = io.WriteString(w, "audio")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream := newHLSStream(ctx, server.URL+"/start.m3u8")
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "audioaudio" || segments.Load() != 2 {
		t.Fatalf("segments = %d, data = %q", segments.Load(), data)
	}
}

func TestHLSFailsOnUnsupportedPlaylistAndCycles(t *testing.T) {
	for _, contents := range []string{
		"#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=key\n",
		"#EXTM3U\n#EXT-X-BYTERANGE:100@0\n",
		"#EXTM3U\n#EXT-X-DISCONTINUITY\n",
		"#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\nindex.m3u8\n",
	} {
		t.Run(contents, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, contents) }))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			stream := newHLSStream(ctx, server.URL+"/index.m3u8")
			defer stream.Close()
			if _, err := io.ReadAll(stream); err == nil {
				t.Fatal("unsupported playlist did not report an error")
			}
		})
	}
}

func TestCloseCancelsStreamReadAndReconnect(t *testing.T) {
	for _, headers := range []bool{false, true} {
		t.Run(fmt.Sprint(headers), func(t *testing.T) {
			entered := make(chan struct{}, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if headers {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				entered <- struct{}{}
				<-r.Context().Done()
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				client := &Client{}
				stream, err := client.OpenStream(ctx, server.URL, nil)
				if err == nil {
					defer stream.Close()
					_, err = stream.Read(make([]byte, 1))
				}
				done <- err
			}()
			<-entered
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("missing cancellation error")
				}
			case <-time.After(time.Second):
				t.Fatal("network read ignored cancellation")
			}
		})
	}
}

func TestEmptySnapshotIsPresent(t *testing.T) {
	client := New(t.TempDir())
	defer client.Close()
	client.queries[queryKey(BrowsePopular, "")] = queryCache{FetchedAt: time.Now()}
	stations, stale := client.Snapshot(BrowsePopular, "")
	if stations == nil || stale {
		t.Fatalf("empty cache treated as missing: %#v %v", stations, stale)
	}
}

func TestHLSAttributePreservesQuotedCommas(t *testing.T) {
	got := hlsAttribute(`#EXT-X-MAP:URI="init.mp4?token=a,b",BYTERANGE="100@0"`, "URI")
	if got != "init.mp4?token=a,b" {
		t.Fatalf("URI = %q", got)
	}
}

func TestImportedStationsSurviveExitOnAnotherSource(t *testing.T) {
	dir := t.TempDir()
	client := New(dir)
	client.AddStations([]Station{{StationUUID: "local-first", URL: "https://one.example/stream"}})
	client.AddStations([]Station{{StationUUID: "local-second", URL: "https://two.example/stream"}})
	client.Close()
	restored := New(dir)
	defer restored.Close()
	for _, id := range []string{"local-first", "local-second"} {
		if _, ok := restored.Lookup("radio:" + id); !ok {
			t.Fatalf("lost imported favorite %s", id)
		}
	}
}

func TestStreamCloseInterruptsReconnectHeaders(t *testing.T) {
	var requests atomic.Int32
	reconnect := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			_, _ = io.WriteString(w, "x")
			return
		}
		close(reconnect)
		<-r.Context().Done()
	}))
	defer server.Close()
	client := &Client{}
	stream, err := client.OpenStream(context.Background(), server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	buffer := make([]byte, 1)
	if _, err := stream.Read(buffer); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := stream.Read(buffer); done <- err }()
	select {
	case <-reconnect:
	case <-time.After(3 * time.Second):
		t.Fatal("reconnect did not start")
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel reconnect")
	}
}
