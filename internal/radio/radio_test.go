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
	"slices"
	"strings"
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
	client.RememberPlayed(station.Path())
	client.queries[queryKey(BrowsePopular, "")] = queryCache{
		Version:  cacheVersion,
		Stations: []Station{station},
	}
	client.MarkDead(station.Path())
	if _, ok := client.Lookup(station.Path()); ok {
		t.Fatal("failed station metadata remains in cache")
	}
	stations, stale := client.Snapshot(BrowsePopular, "")
	if len(stations) != 0 {
		t.Fatalf("dead station remains in query cache: %#v", stations)
	}
	if !stale {
		t.Fatal("dead-station eviction did not invalidate the query cache")
	}
	if history := client.History(); len(history) != 1 || history[0].StationUUID != station.StationUUID {
		t.Fatalf("failed station was removed from history: %#v", history)
	}
}

func TestClientHistoryPersistsDeduplicatedStationsAndCanBeCleared(t *testing.T) {
	dir := t.TempDir()
	client := New(dir)
	first := Station{StationUUID: "first", Name: "First", URL: "https://example.test/first"}
	second := Station{StationUUID: "second", Name: "Second", URL: "https://example.test/second"}
	client.AddTransientStations([]Station{first, second})
	client.RememberPlayed(first.Path())
	client.RememberPlayed(second.Path())
	client.RememberPlayed(first.Path())
	client.Close()

	reloaded := New(dir)
	history := reloaded.History()
	if len(history) != 2 || history[0].StationUUID != "first" || history[1].StationUUID != "second" {
		t.Fatalf("reloaded history = %#v, want first then second", history)
	}
	if history[0].PlayedAt.IsZero() || history[1].PlayedAt.IsZero() {
		t.Fatalf("history timestamps were not persisted: %#v", history)
	}
	reloaded.ClearHistory()
	reloaded.Close()

	cleared := New(dir)
	t.Cleanup(cleared.Close)
	if history := cleared.History(); len(history) != 0 {
		t.Fatalf("cleared history = %#v, want empty", history)
	}
}

func TestClientHistoryIsLimitedToFiftyStations(t *testing.T) {
	client := New(t.TempDir())
	stations := make([]Station, radioHistoryLimit+1)
	for i := range stations {
		stations[i] = Station{
			StationUUID: fmt.Sprintf("station-%02d", i),
			Name:        fmt.Sprintf("Station %02d", i),
			URL:         fmt.Sprintf("https://example.test/%02d", i),
		}
	}
	client.AddTransientStations(stations)
	for _, station := range stations {
		client.RememberPlayed(station.Path())
	}
	history := client.History()
	if len(history) != radioHistoryLimit {
		t.Fatalf("history length = %d, want %d", len(history), radioHistoryLimit)
	}
	if history[0].StationUUID != stations[len(stations)-1].StationUUID || history[len(history)-1].StationUUID != stations[1].StationUUID {
		t.Fatalf("history bounds = %q..%q, want %q..%q", history[0].StationUUID, history[len(history)-1].StationUUID, stations[len(stations)-1].StationUUID, stations[1].StationUUID)
	}
	client.Close()
}

func TestFetchStationPageUsesServerSideAlphabeticalOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("order") != "name" {
			t.Errorf("station order = %q, want name", r.URL.Query().Get("order"))
		}
		var body string
		switch r.URL.Query().Get("reverse") {
		case "false":
			body = `[{"stationuuid":"a","name":"Alpha","url":"https://example.test/a","lastcheckok":1},{"stationuuid":"z","name":"Zulu","url":"https://example.test/z","lastcheckok":1}]`
		case "true":
			body = `[{"stationuuid":"z","name":"Zulu","url":"https://example.test/z","lastcheckok":1},{"stationuuid":"a","name":"Alpha","url":"https://example.test/a","lastcheckok":1}]`
		default:
			t.Errorf("reverse = %q, want false or true", r.URL.Query().Get("reverse"))
			body = `[{"stationuuid":"a","name":"Alpha","url":"https://example.test/a","lastcheckok":1},{"stationuuid":"z","name":"Zulu","url":"https://example.test/z","lastcheckok":1}]`
		}
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()

	client := New(t.TempDir())
	client.servers = []string{server.URL}
	t.Cleanup(client.Close)
	for _, test := range []struct {
		order StationOrder
		want  []string
	}{
		{order: StationOrderAZ, want: []string{"Alpha", "Zulu"}},
		{order: StationOrderZA, want: []string{"Zulu", "Alpha"}},
	} {
		page, err := client.fetchStationPage(context.Background(), BrowsePopular, "", 0, test.order)
		if err != nil {
			t.Fatal(err)
		}
		got := []string{page.stations[0].Name, page.stations[1].Name}
		if !reflect.DeepEqual(got, test.want) {
			t.Fatalf("ordered station page = %v, want %v", got, test.want)
		}
	}
}

func TestClientLoadsCachedFilterValues(t *testing.T) {
	dir := t.TempDir()
	client := New(dir)
	cache := valueCache{Version: cacheVersion, ValueVersion: radioValueVersion, FetchedAt: time.Now(), Values: []string{"rock", "jazz"}, Counts: map[string]int{"rock": 123}}
	client.saveValues(BrowseTag, cache)
	client.Close()

	reloaded := New(dir)
	t.Cleanup(reloaded.Close)
	values, stale := reloaded.Values(BrowseTag)
	if stale || !reflect.DeepEqual(values, []string{"rock", "jazz"}) {
		t.Fatalf("cached values = %#v, stale=%v", values, stale)
	}
	if counts := reloaded.ValueCounts(BrowseTag); counts["rock"] != 123 || counts["jazz"] != 0 {
		t.Fatalf("cached value counts = %#v", counts)
	}
}

func TestFetchValueCountsAcceptsStringAndNumber(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/tags" || r.URL.Query().Get("limit") != "250" {
			t.Fatalf("value request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `[{"name":"rock","stationcount":"123"},{"name":"jazz","stationcount":7}]`)
	}))
	defer server.Close()
	client := New(t.TempDir())
	client.servers = []string{server.URL}
	t.Cleanup(client.Close)
	values, counts, err := client.fetchValuesWithCounts(context.Background(), BrowseTag)
	if err != nil || !reflect.DeepEqual(values, []string{"rock", "jazz"}) || counts["rock"] != 123 || counts["jazz"] != 7 {
		t.Fatalf("values = %#v, counts = %#v, err = %v", values, counts, err)
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
	client.RememberQueueInBrowse(stations[0].Path(), []string{stations[0].Path(), stations[1].Path()}, BrowseTag, "rock")
	client.Close()

	reloaded := New(dir)
	t.Cleanup(reloaded.Close)
	queue := reloaded.LastQueue(stations[0].Path())
	if len(queue) != len(stations) || queue[1].Path() != stations[1].Path() {
		t.Fatalf("remembered queue = %#v, want %#v", queue, stations)
	}
	if kind, filter, ok := reloaded.LastBrowse(stations[0].Path()); !ok || kind != BrowseTag || filter != "rock" {
		t.Fatalf("remembered browse context = %q/%q/%v, want tag/rock/true", kind, filter, ok)
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

func TestPopularQueryHidesBrokenStations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/stations/search" || r.URL.Query().Get("order") != "clickcount" || r.URL.Query().Get("reverse") != "true" || r.URL.Query().Get("offset") != "0" || r.URL.Query().Get("limit") != "100" || r.URL.Query().Get("hidebroken") != "true" {
			t.Fatalf("popular request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, "[]")
	}))
	defer server.Close()

	client := New(t.TempDir())
	client.servers = []string{server.URL}
	t.Cleanup(client.Close)
	if page, err := client.fetchStationPage(context.Background(), BrowsePopular, "", 0, StationOrderSource); err != nil || len(page.stations) != 0 {
		t.Fatalf("fetchStationPage = %#v, %v; want empty result", page, err)
	}
}

func TestResolveStationsFillsMissingStreamURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/stations/byuuid" || r.URL.Query().Get("uuids") != "partial" {
			t.Fatalf("station lookup request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `[{"stationuuid":"partial","name":"Station","url":"https://example.test/stream"}]`)
	}))
	defer server.Close()

	client := New(t.TempDir())
	client.servers = []string{server.URL}
	client.stations["partial"] = Station{StationUUID: "partial", Name: "Station"}
	defer client.Close()

	client.ResolveStations([]string{"radio:partial"})
	select {
	case <-client.resolved:
	case <-time.After(5 * time.Second):
		t.Fatal("station metadata lookup did not complete")
	}
	station, ok := client.Lookup("radio:partial")
	if !ok || station.StreamURL() != "https://example.test/stream" {
		t.Fatalf("resolved station = %+v, %t; want cached stream URL", station, ok)
	}
}

func TestRandomQueryUsesCacheBuster(t *testing.T) {
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = io.WriteString(w, `[{"stationuuid":"random","url":"https://random.example","lastcheckok":1}]`)
	}))
	defer server.Close()

	client := New(t.TempDir())
	client.servers = []string{server.URL}
	t.Cleanup(client.Close)
	if _, err := client.fetchStationPage(context.Background(), BrowseRandom, "", 0, StationOrderSource); err != nil {
		t.Fatal(err)
	}
	values := strings.Split(query, "&")
	if !slices.ContainsFunc(values, func(value string) bool { return strings.HasPrefix(value, "cachebust=") }) {
		t.Fatalf("random query = %q, missing cachebust", query)
	}
}

func TestRandomBeginReplacesPendingAction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "[]")
	}))
	defer server.Close()

	client := New(t.TempDir())
	client.servers = []string{server.URL}
	defer client.Close()
	key := queryKey(BrowseRandom, "")
	oldCtx, oldCancel := context.WithCancel(context.Background())
	defer oldCancel()
	var canceled atomic.Bool
	client.pending[key] = pendingRequest{id: 1, cancel: func() {
		canceled.Store(true)
		oldCancel()
	}}
	client.Begin(context.Background(), BrowseRandom, "")
	if !canceled.Load() || oldCtx.Err() == nil {
		t.Fatal("new random action did not cancel the pending action")
	}
}

func TestStationListsSortByPopularity(t *testing.T) {
	stations := cleanStations([]Station{
		{StationUUID: "low", Name: "Low", URL: "https://low.example", ClickCount: 2, LastCheckOK: 1},
		{StationUUID: "high", Name: "High", URL: "https://high.example", ClickCount: 20, LastCheckOK: 1},
		{StationUUID: "middle", Name: "Middle", URL: "https://middle.example", ClickCount: 10, LastCheckOK: 1},
	})
	if got := []string{stations[0].StationUUID, stations[1].StationUUID, stations[2].StationUUID}; !reflect.DeepEqual(got, []string{"high", "middle", "low"}) {
		t.Fatalf("station order = %#v, want high-to-low popularity", got)
	}
}

func TestPollDiscardsStaleRadioResult(t *testing.T) {
	client := New(t.TempDir())
	t.Cleanup(client.Close)
	client.activeID = 2
	client.results <- result{id: 1, kind: BrowsePopular}
	client.results <- result{id: 2, kind: BrowsePopular, err: errors.New("current request failed")}

	kind, _, _, _, _, err, ok := client.Poll()
	if !ok || kind != BrowsePopular || err == nil {
		t.Fatalf("Poll returned kind=%q err=%v ok=%v, want current error result", kind, err, ok)
	}
}

func TestStationPaginationAppendsAndPersistsCursor(t *testing.T) {
	client := New(t.TempDir())
	t.Cleanup(client.Close)
	key := queryKey(BrowsePopular, "")
	client.queries[key] = queryCache{
		Version:    cacheVersion,
		Stations:   []Station{{StationUUID: "one", URL: "https://one.example"}},
		NextOffset: 100,
		HasMore:    true,
	}
	if offset, ok := client.NextPage(BrowsePopular, ""); !ok || offset != 100 {
		t.Fatalf("NextPage = %d, %v; want 100, true", offset, ok)
	}
	client.activeID = 1
	client.results <- result{
		id:         1,
		key:        key,
		kind:       BrowsePopular,
		offset:     100,
		nextOffset: 200,
		stations:   []Station{{StationUUID: "two", URL: "https://two.example"}},
	}
	_, _, stations, _, _, err, ok := client.Poll()
	if !ok || err != nil || len(stations) != 2 || stations[1].StationUUID != "two" {
		t.Fatalf("appended stations = %#v, err=%v, ok=%v", stations, err, ok)
	}
	if offset, ok := client.NextPage(BrowsePopular, ""); ok || offset != 0 {
		t.Fatalf("NextPage after page without hasMore = %d, %v; want 0, false", offset, ok)
	}
}

func TestRandomResultIsNotCached(t *testing.T) {
	client := New(t.TempDir())
	t.Cleanup(client.Close)
	key := queryKey(BrowseRandom, "")
	client.activeID = 1
	station := Station{StationUUID: "random", URL: "https://random.example"}
	client.results <- result{id: 1, key: key, kind: BrowseRandom, stations: []Station{station}}
	_, _, stations, _, _, err, ok := client.Poll()
	if !ok || err != nil || len(stations) != 1 || stations[0].StationUUID != station.StationUUID {
		t.Fatalf("random result = %#v, err=%v, ok=%v", stations, err, ok)
	}
	if stations, _ := client.Snapshot(BrowseRandom, ""); stations != nil {
		t.Fatal("random result was written to query cache")
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

func TestTransientStationsAreNotPersistedAsImported(t *testing.T) {
	dir := t.TempDir()
	client := New(dir)
	station := Station{StationUUID: "random", URL: "https://random.example/stream"}
	client.AddTransientStations([]Station{station})
	client.Close()

	reloaded := New(dir)
	t.Cleanup(reloaded.Close)
	if _, ok := reloaded.Lookup(station.Path()); ok {
		t.Fatal("transient station was persisted")
	}
}

func TestTransientStationsDoNotLeakIntoLaterImportedCache(t *testing.T) {
	dir := t.TempDir()
	client := New(dir)
	transient := Station{StationUUID: "random", URL: "https://random.example/stream"}
	imported := Station{StationUUID: "playlist", URL: "https://playlist.example/stream"}
	client.AddTransientStations([]Station{transient})
	client.AddStations([]Station{imported})
	client.Close()

	reloaded := New(dir)
	t.Cleanup(reloaded.Close)
	if _, ok := reloaded.Lookup(transient.Path()); ok {
		t.Fatal("transient station leaked into imported cache")
	}
	if _, ok := reloaded.Lookup(imported.Path()); !ok {
		t.Fatal("imported station was not persisted")
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
