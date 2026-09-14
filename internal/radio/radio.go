// Package radio provides the provider-neutral radio directory and stream
// metadata services used by the Radio source.
package radio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dendec/glitchscope/internal/util"
)

const (
	cacheVersion          = 3
	cacheTTL              = 30 * 24 * time.Hour
	radioPageSize         = 100
	radioRequestTimeout   = 12 * time.Second
	radioDirectoryTimeout = 6 * time.Second
	lastStationFile       = "last-station.json"
	radioBrowserAllHost   = "all.api.radio-browser.info"
)

var radioBrowserFallbackServers = []string{"https://de1.api.radio-browser.info"}

// BrowseKind identifies a Radio Browser station query.
type BrowseKind string

const (
	BrowsePopular  BrowseKind = "popular"
	BrowseRandom   BrowseKind = "random"
	BrowseTag      BrowseKind = "tag"
	BrowseLanguage BrowseKind = "language"
	BrowseCountry  BrowseKind = "country"
)

// Station is the stable subset of Radio Browser station metadata needed by
// the UI and playback layer. The JSON field names mirror the API.
type Station struct {
	StationUUID string `json:"stationuuid"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	URLResolved string `json:"url_resolved"`
	Homepage    string `json:"homepage"`
	Favicon     string `json:"favicon"`
	Tags        string `json:"tags"`
	Country     string `json:"country"`
	CountryCode string `json:"countrycode"`
	Language    string `json:"language"`
	Codec       string `json:"codec"`
	Bitrate     int    `json:"bitrate"`
	Votes       int    `json:"votes"`
	ClickCount  int    `json:"clickcount"`
	LastCheckOK int    `json:"lastcheckok"`
}

// Path returns a stable virtual path suitable for Player and favorites.
func (s Station) Path() string { return "radio:" + s.StationUUID }

// StreamURL returns the best available URL.
func (s Station) StreamURL() string {
	if resolved := strings.TrimSpace(s.URLResolved); resolved != "" {
		return resolved
	}
	return strings.TrimSpace(s.URL)
}

// DisplayName returns a useful fallback when a directory row has no name.
func (s Station) DisplayName() string {
	if name := cleanText(s.Name); name != "" {
		return name
	}
	return cleanText(s.StreamURL())
}

type queryCache struct {
	Version    int       `json:"version"`
	FetchedAt  time.Time `json:"fetched_at"`
	Stations   []Station `json:"stations"`
	NextOffset int       `json:"next_offset,omitempty"`
	HasMore    bool      `json:"has_more,omitempty"`
}

type valueCache struct {
	Version   int            `json:"version"`
	FetchedAt time.Time      `json:"fetched_at"`
	Values    []string       `json:"values"`
	Counts    map[string]int `json:"counts,omitempty"`
}

type stationCache struct {
	Version      int        `json:"version"`
	Station      Station    `json:"station"`
	Stations     []Station  `json:"stations,omitempty"`
	BrowseKind   BrowseKind `json:"browse_kind,omitempty"`
	BrowseFilter string     `json:"browse_filter,omitempty"`
}

type serverRecord struct {
	Name string `json:"name"`
}

// Client is a cached Radio Browser client. Network work is always started by
// Begin; Snapshot and Values are cheap and safe to call from the UI thread.
type Client struct {
	baseDir    string
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	ioMu       sync.Mutex
	saveMu     sync.Mutex
	saveTail   chan struct{}
	saveClosed bool

	mu               sync.RWMutex
	closed           bool
	servers          []string
	queries          map[string]queryCache
	stations         map[string]Station
	imported         map[string]struct{}
	values           map[BrowseKind]valueCache
	pending          map[string]pendingRequest
	results          chan result
	lastQueue        []Station
	lastBrowseKind   BrowseKind
	lastBrowseFilter string
	lastStationID    string
	requestID        uint64
	activeID         uint64
}

type pendingRequest struct {
	id     uint64
	cancel context.CancelFunc
}

type result struct {
	id         uint64
	key        string
	kind       BrowseKind
	filter     string
	offset     int
	nextOffset int
	stations   []Station
	values     []string
	counts     map[string]int
	hasMore    bool
	err        error
}

type stationPage struct {
	stations   []Station
	nextOffset int
	hasMore    bool
}

// New creates a client and loads only local metadata. It never performs a
// network request synchronously.
func New(baseDir string) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		baseDir:  baseDir,
		queries:  make(map[string]queryCache),
		stations: make(map[string]Station),
		imported: make(map[string]struct{}),
		values:   make(map[BrowseKind]valueCache),
		pending:  make(map[string]pendingRequest),
		results:  make(chan result, 8),
		ctx:      ctx,
		cancel:   cancel,
	}
	c.loadCache()
	return c
}

func (c *Client) cacheDir() string { return filepath.Join(c.baseDir, ".cache", "radio") }

func (c *Client) loadCache() {
	entries, err := os.ReadDir(c.cacheDir())
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("radio cache read failed", "error", err)
		}
		return
	}
	for _, entry := range entries {
		if entry.Name() == lastStationFile {
			data, readErr := os.ReadFile(filepath.Join(c.cacheDir(), entry.Name()))
			if readErr != nil {
				continue
			}
			var cache stationCache
			if json.Unmarshal(data, &cache) == nil && cache.Version == cacheVersion {
				c.lastBrowseKind = cache.BrowseKind
				c.lastBrowseFilter = strings.TrimSpace(cache.BrowseFilter)
				station := cleanStation(cache.Station)
				if station.StationUUID != "" && station.StreamURL() != "" {
					c.lastStationID = station.StationUUID
					c.stations[station.StationUUID] = station
				}
				queue := cleanQueueStations(append([]Station{station}, cache.Stations...))
				if len(queue) > 0 {
					c.lastQueue = queue
					for _, queued := range queue {
						c.stations[queued.StationUUID] = queued
					}
				}
			}
			continue
		}
		if strings.HasPrefix(entry.Name(), "values-") && strings.HasSuffix(entry.Name(), ".json") {
			data, readErr := os.ReadFile(filepath.Join(c.cacheDir(), entry.Name()))
			if readErr != nil {
				continue
			}
			var cache valueCache
			if json.Unmarshal(data, &cache) == nil && cache.Version == cacheVersion {
				kind := BrowseKind(strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "values-"), ".json"))
				c.values[kind] = cache
			}
			continue
		}
		if !strings.HasPrefix(entry.Name(), "query-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(c.cacheDir(), entry.Name()))
		if err != nil {
			continue
		}
		var cache queryCache
		if json.Unmarshal(data, &cache) != nil || cache.Version != cacheVersion {
			continue
		}
		if entry.Name() == "query-imported.json" {
			cache.Stations = cleanQueueStations(cache.Stations)
		} else {
			cache.Stations = cleanStations(cache.Stations)
		}
		key := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "query-"), ".json")
		if key != "imported" {
			c.queries[key] = cache
		}
		for _, station := range cache.Stations {
			c.stations[station.StationUUID] = station
			if key == "imported" {
				c.imported[station.StationUUID] = struct{}{}
			}
		}
	}
}

func (c *Client) saveQuery(key string, cache queryCache) {
	c.saveJSON("query-"+key+".json", cache)
}

func (c *Client) saveValues(kind BrowseKind, cache valueCache) {
	c.saveJSON("values-"+string(kind)+".json", cache)
}

func (c *Client) saveJSON(name string, value any) {
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	if err := os.MkdirAll(c.cacheDir(), 0o755); err != nil {
		slog.Warn("radio cache mkdir failed", "error", err)
		return
	}
	data, err := json.Marshal(value)
	if err != nil {
		slog.Warn("radio cache encode failed", "name", name, "error", err)
		return
	}
	tmp := filepath.Join(c.cacheDir(), "."+name+".tmp")
	path := filepath.Join(c.cacheDir(), name)
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		slog.Warn("radio cache write failed", "name", name, "error", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		slog.Warn("radio cache rename failed", "name", name, "error", err)
	}
}

func (c *Client) removeJSON(name string) {
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	if err := os.Remove(filepath.Join(c.cacheDir(), name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("radio cache remove failed", "name", name, "error", err)
	}
}

func queryKey(kind BrowseKind, filter string) string {
	return string(kind) + "-" + url.PathEscape(strings.ToLower(strings.TrimSpace(filter)))
}

func validBrowseContext(kind BrowseKind, filter string) bool {
	switch kind {
	case BrowsePopular:
		return filter == ""
	case BrowseTag, BrowseLanguage, BrowseCountry:
		return filter != ""
	default:
		return false
	}
}

// Snapshot returns cached stations for a query and whether that snapshot is
// stale. A stale snapshot remains usable while a background refresh runs.
func (c *Client) Snapshot(kind BrowseKind, filter string) (stations []Station, stale bool) {
	key := queryKey(kind, filter)
	c.mu.RLock()
	cache, ok := c.queries[key]
	c.mu.RUnlock()
	if !ok {
		return nil, true
	}
	return append([]Station{}, cache.Stations...), time.Since(cache.FetchedAt) > cacheTTL
}

// NextPage returns the offset for the next cached station page. It is false
// when the query has no cached page or the server reported that it is complete.
func (c *Client) NextPage(kind BrowseKind, filter string) (offset int, ok bool) {
	if kind == BrowseRandom {
		return 0, false
	}
	key := queryKey(kind, filter)
	c.mu.RLock()
	cache, exists := c.queries[key]
	c.mu.RUnlock()
	if !exists || !cache.HasMore {
		return 0, false
	}
	if cache.NextOffset > 0 {
		return cache.NextOffset, true
	}
	return len(cache.Stations), true
}

// HasMore reports whether a cached query has another station page.
func (c *Client) HasMore(kind BrowseKind, filter string) bool {
	_, ok := c.NextPage(kind, filter)
	return ok
}

// Values returns cached tag/language/country filters and whether they are stale.
func (c *Client) Values(kind BrowseKind) (values []string, stale bool) {
	c.mu.RLock()
	cache, ok := c.values[kind]
	c.mu.RUnlock()
	if !ok {
		return nil, true
	}
	values = append([]string{}, cache.Values...)
	sort.SliceStable(values, func(i, j int) bool {
		left, right := cache.Counts[values[i]], cache.Counts[values[j]]
		if left != right {
			return left > right
		}
		return strings.ToLower(values[i]) < strings.ToLower(values[j])
	})
	return values, time.Since(cache.FetchedAt) > cacheTTL
}

// ValueCounts returns the station counts reported for cached filter values.
func (c *Client) ValueCounts(kind BrowseKind) map[string]int {
	c.mu.RLock()
	counts := c.values[kind].Counts
	c.mu.RUnlock()
	return copyCounts(counts)
}

// Lookup resolves a radio virtual path to its cached stream URL.
func (c *Client) Lookup(path string) (Station, bool) {
	id := strings.TrimPrefix(path, "radio:")
	c.mu.RLock()
	station, ok := c.stations[id]
	c.mu.RUnlock()
	return station, ok
}

func (c *Client) addStationsLocked(stations []Station) {
	for _, station := range stations {
		station = cleanStation(station)
		if station.StreamURL() == "" {
			continue
		}
		if station.StationUUID == "" {
			station.StationUUID = playlistUUID(station.StreamURL())
		}
		c.stations[station.StationUUID] = station
	}
}

// AddTransientStations adds stations to the in-memory lookup table without
// writing a directory or imported-playlist cache. Radio Browser selections and
// one-shot random playback use this path; their query/last-position caches are
// managed independently.
func (c *Client) AddTransientStations(stations []Station) {
	c.mu.Lock()
	c.addStationsLocked(stations)
	c.mu.Unlock()
}

// AddStations adds stations from a user playlist to the local lookup table and
// persists the imported descriptors for later playback.
func (c *Client) AddStations(stations []Station) {
	c.mu.Lock()
	c.addStationsLocked(stations)
	// Persist descriptors independently of the last playing source: favorites
	// from imported playlists must still resolve after exiting on a local track.
	// Keep the provenance explicit so transient Radio Browser/random stations do
	// not leak into the imported cache on a later playlist import.
	for _, station := range stations {
		station = cleanStation(station)
		if station.StreamURL() == "" {
			continue
		}
		if station.StationUUID == "" {
			station.StationUUID = playlistUUID(station.StreamURL())
		}
		c.imported[station.StationUUID] = struct{}{}
	}
	saved := make([]Station, 0, len(c.imported))
	for id := range c.imported {
		if station, ok := c.stations[id]; ok {
			saved = append(saved, station)
		}
	}
	c.mu.Unlock()
	c.saveAsync(func() {
		c.saveQuery("imported", queryCache{Version: cacheVersion, FetchedAt: time.Now(), Stations: saved})
	})
}

// RememberStation stores enough metadata to restore a radio stream after the
// next application start, including stations from user playlists that have no
// Radio Browser query cache.
func (c *Client) RememberStation(path string) {
	c.RememberQueue(path, nil)
}

// RememberQueue persists the active radio queue along with its current
// station, so a failed restored stream can continue with the same next/random
// station semantics after a restart.
func (c *Client) RememberQueue(path string, paths []string) {
	c.RememberQueueInBrowse(path, paths, "", "")
}

// RememberQueueInBrowse persists the active radio queue and the directory
// context from which its current station was selected.
func (c *Client) RememberQueueInBrowse(path string, paths []string, kind BrowseKind, filter string) {
	id := strings.TrimPrefix(path, "radio:")
	filter = strings.TrimSpace(filter)
	if !validBrowseContext(kind, filter) {
		kind, filter = "", ""
	}
	c.mu.RLock()
	station, ok := c.stations[id]
	queue := make([]Station, 0, len(paths))
	for _, queuedPath := range paths {
		queuedID := strings.TrimPrefix(queuedPath, "radio:")
		if queued, exists := c.stations[queuedID]; exists {
			queue = append(queue, queued)
		}
	}
	c.mu.RUnlock()

	station = cleanStation(station)
	if !ok || station.StationUUID == "" || station.StreamURL() == "" {
		return
	}
	queue = cleanQueueStations(queue)
	if len(queue) == 0 {
		queue = []Station{station}
	}
	c.mu.Lock()
	c.lastQueue = append([]Station(nil), queue...)
	c.lastStationID = station.StationUUID
	c.lastBrowseKind = kind
	c.lastBrowseFilter = filter
	c.mu.Unlock()
	c.saveJSON(lastStationFile, stationCache{
		Version:      cacheVersion,
		Station:      station,
		Stations:     queue,
		BrowseKind:   kind,
		BrowseFilter: filter,
	})
}

// LastQueue returns the queue saved with the requested current station.
func (c *Client) LastQueue(path string) []Station {
	id := strings.TrimPrefix(path, "radio:")
	c.mu.RLock()
	defer c.mu.RUnlock()
	if id == "" || len(c.lastQueue) == 0 {
		return nil
	}
	for _, station := range c.lastQueue {
		if station.StationUUID == id {
			return append([]Station(nil), c.lastQueue...)
		}
	}
	return nil
}

// LastBrowse returns the Radio Browser category and filter used for the saved
// station, when that context is known and can be restored from the directory.
func (c *Client) LastBrowse(path string) (BrowseKind, string, bool) {
	id := strings.TrimPrefix(path, "radio:")
	c.mu.RLock()
	defer c.mu.RUnlock()
	if id == "" || c.lastStationID != id {
		return "", "", false
	}
	if !validBrowseContext(c.lastBrowseKind, c.lastBrowseFilter) {
		return "", "", false
	}
	return c.lastBrowseKind, c.lastBrowseFilter, true
}

// MarkDead removes a failed station from all local radio caches. The next
// directory refresh can reintroduce it if Radio Browser reports it healthy.
func (c *Client) MarkDead(path string) {
	id := strings.TrimPrefix(path, "radio:")
	if id == "" {
		return
	}
	c.mu.Lock()
	delete(c.stations, id)
	delete(c.imported, id)
	for key, cache := range c.queries {
		filtered := make([]Station, 0, len(cache.Stations))
		changed := false
		for _, station := range cache.Stations {
			if station.StationUUID == id {
				changed = true
				continue
			}
			filtered = append(filtered, station)
		}
		if changed {
			cache.Stations = append([]Station(nil), filtered...)
			cache.FetchedAt = time.Time{}
			c.queries[key] = cache
			cacheKey, cacheCopy := key, cache
			c.saveAsync(func() { c.saveQuery(cacheKey, cacheCopy) })
		}
	}
	filteredQueue := c.lastQueue[:0]
	for _, station := range c.lastQueue {
		if station.StationUUID != id {
			filteredQueue = append(filteredQueue, station)
		}
	}
	c.lastQueue = append([]Station(nil), filteredQueue...)
	queueCopy := append([]Station(nil), filteredQueue...)
	browseKind, browseFilter := c.lastBrowseKind, c.lastBrowseFilter
	if len(queueCopy) == 0 {
		c.lastStationID = ""
		c.lastBrowseKind, c.lastBrowseFilter = "", ""
	} else {
		c.lastStationID = queueCopy[0].StationUUID
	}
	remaining := make([]Station, 0, len(c.imported))
	for importedID := range c.imported {
		if station, ok := c.stations[importedID]; ok {
			remaining = append(remaining, station)
		}
	}
	c.mu.Unlock()
	c.saveAsync(func() {
		c.saveQuery("imported", queryCache{Version: cacheVersion, FetchedAt: time.Now(), Stations: remaining})
		if len(queueCopy) == 0 {
			c.removeJSON(lastStationFile)
			return
		}
		c.saveJSON(lastStationFile, stationCache{
			Version:      cacheVersion,
			Station:      queueCopy[0],
			Stations:     queueCopy,
			BrowseKind:   browseKind,
			BrowseFilter: browseFilter,
		})
	})
}

// Begin starts an asynchronous station query. Cached data is available from
// Snapshot immediately; callers should Poll results once per frame.
func (c *Client) Begin(ctx context.Context, kind BrowseKind, filter string) {
	c.BeginPage(ctx, kind, filter, 0)
}

// BeginPage starts an asynchronous station query for one page. Offset zero
// refreshes the visible page; later offsets append to its cached listing.
func (c *Client) BeginPage(ctx context.Context, kind BrowseKind, filter string, offset int) {
	if kind == BrowseRandom {
		offset = 0
	}
	key := queryKey(kind, filter)
	requestCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	if c.closed {
		cancel()
		c.mu.Unlock()
		return
	}
	if pending, ok := c.pending[key]; ok {
		if kind != BrowseRandom {
			cancel()
			c.mu.Unlock()
			return
		}
		// Random is an action rather than a cacheable query. Replace an
		// in-flight action so a quick second press cannot replay or wait for
		// the first station.
		pending.cancel()
		delete(c.pending, key)
	}
	// Only the currently visible query is useful to the UI. Cancel work for
	// the previous view so a slow server cannot accumulate stale requests.
	for pendingKey, pending := range c.pending {
		if pendingKey == key {
			continue
		}
		pending.cancel()
		delete(c.pending, pendingKey)
	}
	c.requestID++
	id := c.requestID
	c.activeID = id
	c.pending[key] = pendingRequest{id: id, cancel: cancel}
	// Register the worker before releasing mu. Close also takes mu before
	// waiting on the group, so it can never observe a zero counter and race a
	// concurrent BeginPage Add.
	c.wg.Add(1)
	c.mu.Unlock()
	go func() {
		defer c.wg.Done()
		defer cancel()
		defer func() {
			if requestCtx.Err() == nil {
				return
			}
			c.mu.Lock()
			if pending, ok := c.pending[key]; ok && pending.id == id {
				delete(c.pending, key)
			}
			c.mu.Unlock()
		}()
		stop := context.AfterFunc(c.ctx, cancel)
		defer stop()
		var page stationPage
		var values []string
		var counts map[string]int
		var err error
		if kind == BrowseTag || kind == BrowseLanguage || kind == BrowseCountry {
			if filter == "" {
				values, counts, err = c.fetchValuesWithCounts(requestCtx, kind)
			} else {
				page, err = c.fetchStationPage(requestCtx, kind, filter, offset)
			}
		} else {
			page, err = c.fetchStationPage(requestCtx, kind, filter, offset)
		}
		if requestCtx.Err() != nil {
			return
		}
		select {
		case c.results <- result{id: id, key: key, kind: kind, filter: filter, offset: offset, nextOffset: page.nextOffset, stations: page.stations, values: values, counts: counts, hasMore: page.hasMore, err: err}:
		case <-c.ctx.Done():
		case <-requestCtx.Done():
		}
	}()
}

// Poll applies one completed background request. It returns the query that
// changed so the app can rebuild only that navigation level.
func (c *Client) Poll() (kind BrowseKind, filter string, stations []Station, values []string, hasMore bool, err error, ok bool) {
	for {
		select {
		case result := <-c.results:
			c.mu.Lock()
			if result.id != c.activeID {
				c.mu.Unlock()
				continue
			}
			if pending, exists := c.pending[result.key]; exists && pending.id == result.id {
				delete(c.pending, result.key)
			}
			if result.err == nil && result.kind != BrowseRandom {
				isValueQuery := result.filter == "" && (result.kind == BrowseTag || result.kind == BrowseLanguage || result.kind == BrowseCountry)
				if isValueQuery {
					cache := valueCache{Version: cacheVersion, FetchedAt: time.Now(), Values: append([]string(nil), result.values...), Counts: copyCounts(result.counts)}
					c.values[result.kind] = cache
					c.saveAsync(func() { c.saveValues(result.kind, cache) })
				} else {
					cache := c.queries[result.key]
					if result.offset == 0 {
						cache.Stations = nil
					}
					cache.Version = cacheVersion
					cache.FetchedAt = time.Now()
					cache.Stations = appendStations(cache.Stations, result.stations)
					cache.NextOffset = result.nextOffset
					if result.offset == 0 && len(result.stations) == 0 {
						cache.NextOffset = 0
					}
					cache.HasMore = result.hasMore
					c.queries[result.key] = cache
					for _, station := range result.stations {
						c.stations[station.StationUUID] = station
					}
					c.saveAsync(func() { c.saveQuery(result.key, cache) })
				}
			}
			c.mu.Unlock()
			stations = append([]Station(nil), result.stations...)
			isValueQuery := result.filter == "" && (result.kind == BrowseTag || result.kind == BrowseLanguage || result.kind == BrowseCountry)
			if result.err == nil && result.kind != BrowseRandom && !isValueQuery {
				c.mu.RLock()
				if cache, exists := c.queries[result.key]; exists {
					stations = append([]Station(nil), cache.Stations...)
				}
				c.mu.RUnlock()
			}
			return result.kind, result.filter, stations, result.values, result.hasMore, result.err, true
		default:
			return "", "", nil, nil, false, nil, false
		}
	}
}

func (c *Client) saveAsync(save func()) {
	// Serialize in submission order without making the UI wait for disk I/O.
	c.saveMu.Lock()
	if c.saveClosed {
		c.saveMu.Unlock()
		return
	}
	previous, done := c.saveTail, make(chan struct{})
	c.saveTail = done
	c.wg.Add(1)
	c.saveMu.Unlock()
	go func() {
		defer c.wg.Done()
		defer close(done)
		if previous != nil {
			<-previous
		}
		save()
	}()
}

// Close cancels directory requests and waits for requests and cache writes.
func (c *Client) Close() {
	c.cancel()
	c.mu.Lock()
	c.closed = true
	for key, pending := range c.pending {
		pending.cancel()
		delete(c.pending, key)
	}
	c.mu.Unlock()
	c.saveMu.Lock()
	c.saveClosed = true
	c.saveMu.Unlock()
	c.wg.Wait()
}

func (c *Client) fetchStations(ctx context.Context, kind BrowseKind, filter string) ([]Station, error) {
	page, err := c.fetchStationPage(ctx, kind, filter, 0)
	return page.stations, err
}

func (c *Client) fetchStationPage(ctx context.Context, kind BrowseKind, filter string, offset int) (stationPage, error) {
	if offset < 0 {
		return stationPage{}, fmt.Errorf("radio: invalid station offset %d", offset)
	}
	path := ""
	switch kind {
	case BrowsePopular:
		path = fmt.Sprintf("/json/stations/search?order=clickcount&reverse=true&offset=%d&limit=%d&hidebroken=true", offset, radioPageSize)
	case BrowseRandom:
		// Some API mirrors/CDNs cache GET responses by URL. A unique query
		// value keeps successive random actions from replaying the same station.
		path = fmt.Sprintf("/json/stations/search?order=random&limit=1&hidebroken=true&cachebust=%d", time.Now().UnixNano())
	case BrowseTag:
		path = fmt.Sprintf("/json/stations/bytagexact/%s?order=clickcount&reverse=true&offset=%d&limit=%d&hidebroken=true", url.PathEscape(filter), offset, radioPageSize)
	case BrowseLanguage:
		path = fmt.Sprintf("/json/stations/bylanguageexact/%s?order=clickcount&reverse=true&offset=%d&limit=%d&hidebroken=true", url.PathEscape(filter), offset, radioPageSize)
	case BrowseCountry:
		path = fmt.Sprintf("/json/stations/bycountryexact/%s?order=clickcount&reverse=true&offset=%d&limit=%d&hidebroken=true", url.PathEscape(filter), offset, radioPageSize)
	default:
		return stationPage{}, fmt.Errorf("radio: unknown browse kind %q", kind)
	}
	var raw []Station
	if err := c.getJSONFromServers(ctx, path, &raw, radioDirectoryTimeout); err != nil {
		return stationPage{}, err
	}
	stations := cleanStations(raw)
	return stationPage{stations: stations, nextOffset: offset + len(raw), hasMore: kind != BrowseRandom && len(raw) >= radioPageSize}, nil
}

func appendStations(existing, incoming []Station) []Station {
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	result := make([]Station, 0, len(existing)+len(incoming))
	appendStation := func(station Station) {
		key := station.StationUUID
		if key == "" {
			key = station.StreamURL()
		}
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		result = append(result, station)
	}
	for _, station := range existing {
		appendStation(station)
	}
	for _, station := range incoming {
		appendStation(station)
	}
	sortStationsByPopularity(result)
	return result
}

func (c *Client) fetchValuesWithCounts(ctx context.Context, kind BrowseKind) ([]string, map[string]int, error) {
	path := map[BrowseKind]string{
		BrowseTag:      "/json/tags",
		BrowseLanguage: "/json/languages",
		BrowseCountry:  "/json/countries",
	}[kind]
	if path == "" {
		return nil, nil, fmt.Errorf("radio: %s has no filter values", kind)
	}
	var rows []struct {
		Name         string          `json:"name"`
		StationCount json.RawMessage `json:"stationcount"`
	}
	if err := c.getJSONFromServers(ctx, path+"?order=stationcount&reverse=true&limit=250", &rows, radioDirectoryTimeout); err != nil {
		return nil, nil, err
	}
	values := make([]string, 0, len(rows))
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name != "" {
			values = append(values, name)
			if count := parseStationCount(row.StationCount); count > 0 {
				counts[name] = count
			}
		}
	}
	return values, counts, nil
}

func parseStationCount(raw json.RawMessage) int {
	value := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	count, err := strconv.Atoi(value)
	if err != nil || count < 0 {
		return 0
	}
	return count
}

func copyCounts(counts map[string]int) map[string]int {
	if len(counts) == 0 {
		return nil
	}
	copy := make(map[string]int, len(counts))
	for key, value := range counts {
		copy[key] = value
	}
	return copy
}

func (c *Client) getJSONTimeout(ctx context.Context, endpoint string, dst any, timeout time.Duration) error {
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := util.Get(requestCtx, endpoint, nil)
	if err != nil {
		return fmt.Errorf("radio browser: GET %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("radio browser: GET %s: HTTP %s", endpoint, resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(dst); err != nil {
		return fmt.Errorf("radio browser: decode %s: %w", endpoint, err)
	}
	return nil
}

func (c *Client) getJSONFromServers(ctx context.Context, path string, dst any, timeout time.Duration) error {
	if strings.TrimSpace(path) == "" || !strings.HasPrefix(path, "/") {
		return fmt.Errorf("radio browser: invalid API path %q", path)
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	servers, err := c.serversForRequest(requestCtx, timeout)
	if err != nil {
		return err
	}
	order := rand.Perm(len(servers))
	errs := make([]error, 0, len(order))
	for _, index := range order {
		base := strings.TrimRight(servers[index], "/")
		endpoint := base + path
		if err := c.getJSONTimeout(requestCtx, endpoint, dst, timeout); err == nil {
			return nil
		} else {
			errs = append(errs, err)
			if requestCtx.Err() == nil {
				c.invalidateServer(base)
			}
		}
	}
	if len(errs) == 0 {
		return errors.New("radio browser: no API servers available")
	}
	return errors.Join(errs...)
}

// FetchFavicon downloads one station icon. The caller owns the returned bytes;
// image decoding runs in a background worker before GL texture upload.
func (c *Client) FetchFavicon(ctx context.Context, targetURL string) ([]byte, error) {
	if strings.TrimSpace(targetURL) == "" {
		return nil, nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, radioRequestTimeout)
	defer cancel()
	resp, err := util.Get(requestCtx, targetURL, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("radio favicon: HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read radio favicon: %w", err)
	}
	return data, nil
}

func (c *Client) serversForRequest(ctx context.Context, timeout time.Duration) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.RLock()
	servers := append([]string(nil), c.servers...)
	c.mu.RUnlock()
	if len(servers) > 0 {
		return servers, nil
	}

	// Discovery is best-effort. Keep it short so a slow reverse-DNS/SRV
	// resolver cannot consume the whole directory request and leave no time for
	// the known fallback mirror.
	discoveryCtx, cancel := context.WithTimeout(ctx, min(timeout, 2*time.Second))
	discovered, err := c.discoverServers(discoveryCtx, min(timeout, 2*time.Second))
	cancel()
	if len(discovered) == 0 {
		discovered = append([]string(nil), radioBrowserFallbackServers...)
		slog.Warn("radio browser server discovery failed; using fallback", "error", err, "servers", discovered)
	} else if err != nil {
		slog.Warn("radio browser server discovery partially failed", "error", err, "servers", discovered)
	}
	// Keep one known API endpoint in the rotation even when discovery returns
	// another mirror. A transiently stale DNS/SRV answer must not make the
	// directory unavailable for the whole request.
	discovered = append(discovered, radioBrowserFallbackServers...)
	servers = uniqueServers(discovered)
	if len(servers) == 0 {
		return nil, errors.New("radio browser: server discovery returned no usable HTTPS servers")
	}
	c.mu.Lock()
	c.servers = append([]string(nil), servers...)
	c.mu.Unlock()
	return servers, nil
}

func (c *Client) discoverServers(ctx context.Context, timeout time.Duration) ([]string, error) {
	servers := make([]string, 0, 4)
	seen := make(map[string]struct{})
	add := func(raw string) {
		server, ok := normalizeServerURL(raw)
		if !ok {
			return
		}
		if _, exists := seen[server]; exists {
			return
		}
		seen[server] = struct{}{}
		servers = append(servers, server)
	}
	errs := make([]error, 0, 3)

	lookupCtx, cancel := context.WithTimeout(ctx, min(timeout, 2*time.Second))
	ips, err := net.DefaultResolver.LookupHost(lookupCtx, radioBrowserAllHost)
	if err != nil {
		errs = append(errs, fmt.Errorf("DNS lookup %s: %w", radioBrowserAllHost, err))
	} else {
		for _, ip := range ips {
			names, lookupErr := net.DefaultResolver.LookupAddr(lookupCtx, ip)
			if lookupErr != nil {
				errs = append(errs, fmt.Errorf("reverse DNS lookup %s: %w", ip, lookupErr))
				continue
			}
			for _, name := range names {
				name = strings.TrimSuffix(strings.TrimSpace(name), ".")
				if strings.HasSuffix(strings.ToLower(name), ".api.radio-browser.info") {
					add(name)
				}
			}
		}
	}
	cancel()

	if len(servers) == 0 {
		_, records, srvErr := net.DefaultResolver.LookupSRV(ctx, "api", "tcp", "radio-browser.info")
		if srvErr != nil {
			errs = append(errs, fmt.Errorf("SRV lookup _api._tcp.radio-browser.info: %w", srvErr))
		} else {
			for _, record := range records {
				add(strings.TrimSuffix(record.Target, "."))
			}
		}
	}

	if len(servers) == 0 {
		var records []serverRecord
		if err := c.getJSONTimeout(ctx, "https://"+radioBrowserAllHost+"/json/servers", &records, timeout); err != nil {
			errs = append(errs, fmt.Errorf("HTTPS server list: %w", err))
		} else {
			for _, record := range records {
				add(record.Name)
			}
		}
	}
	if len(errs) == 0 {
		if len(servers) == 0 {
			return nil, errors.New("server discovery returned no usable records")
		}
		return servers, nil
	}
	return servers, errors.Join(errs...)
}

func normalizeServerURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "api.radio-browser.info" && !strings.HasSuffix(host, ".api.radio-browser.info") {
		return "", false
	}
	return strings.TrimRight(parsed.String(), "/"), true
}

func uniqueServers(servers []string) []string {
	unique := make([]string, 0, len(servers))
	seen := make(map[string]struct{}, len(servers))
	for _, raw := range servers {
		server, ok := normalizeServerURL(raw)
		if !ok {
			continue
		}
		if _, exists := seen[server]; exists {
			continue
		}
		seen[server] = struct{}{}
		unique = append(unique, server)
	}
	return unique
}

func (c *Client) invalidateServer(server string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	filtered := c.servers[:0]
	for _, candidate := range c.servers {
		if candidate != server {
			filtered = append(filtered, candidate)
		}
	}
	c.servers = append([]string(nil), filtered...)
}

// Click notifies Radio Browser that a station was started. Failure is
// deliberately non-fatal to playback.
func (c *Client) Click(ctx context.Context, stationUUID string) error {
	if stationUUID == "" {
		return errors.New("radio: empty station UUID")
	}
	var response struct {
		OK      any    `json:"ok"`
		Message string `json:"message"`
	}
	return c.getJSONFromServers(ctx, "/json/url/"+url.PathEscape(stationUUID), &response, radioRequestTimeout)
}

// OpenStream opens one provider-owned audio stream. HTTP and ICY framing stay
// in Go; the returned reader exposes only compressed audio bytes to the
// decoder. StreamTitle metadata is emitted while the same connection is read,
// so playback does not need a second metadata connection.
func (c *Client) OpenStream(ctx context.Context, streamURL string, emit func(string)) (io.ReadCloser, error) {
	if strings.TrimSpace(streamURL) == "" {
		return nil, errors.New("radio: empty stream URL")
	}
	if isHLSURL(streamURL) {
		return newHLSStream(ctx, streamURL), nil
	}
	streamCtx, cancel := context.WithCancel(ctx)
	body, err := openStreamOnce(streamCtx, streamURL, emit)
	if err != nil {
		cancel()
		return nil, err
	}
	return &reconnectingStream{
		cancel: cancel,
		ctx:    streamCtx,
		url:    streamURL,
		emit:   emit,
		body:   body,
	}, nil
}

func isHLSURL(streamURL string) bool {
	parsed, err := url.Parse(streamURL)
	return err == nil && strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8")
}

type hlsSegment struct {
	sequence uint64
	url      string
}

type hlsPlaylist struct {
	variant        string
	initSegment    string
	segments       []hlsSegment
	targetDuration time.Duration
	end            bool
}

type hlsStream struct {
	reader *io.PipeReader
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func newHLSStream(parent context.Context, playlistURL string) io.ReadCloser {
	ctx, cancel := context.WithCancel(parent)
	reader, writer := io.Pipe()
	stream := &hlsStream{reader: reader, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(stream.done)
		defer func() { _ = writer.Close() }()
		runHLS(ctx, writer, playlistURL)
	}()
	return stream
}

func (s *hlsStream) Read(buffer []byte) (int, error) { return s.reader.Read(buffer) }

func (s *hlsStream) Close() error {
	var err error
	s.once.Do(func() {
		s.cancel()
		err = s.reader.Close()
	})
	<-s.done
	return err
}

func runHLS(ctx context.Context, writer *io.PipeWriter, playlistURL string) {
	var nextSequence uint64
	started := false
	initURL := ""
	variants := make(map[string]bool)
	for ctx.Err() == nil {
		playlist, err := fetchHLSPlaylist(ctx, playlistURL)
		if err != nil {
			_ = writer.CloseWithError(err)
			return
		}
		if playlist.variant != "" {
			if variants[playlist.variant] || len(variants) >= 8 {
				_ = writer.CloseWithError(errors.New("HLS variant cycle or depth limit"))
				return
			}
			variants[playlist.variant] = true
			playlistURL = playlist.variant
			continue
		}
		if playlist.initSegment != initURL {
			if initURL != "" {
				_ = writer.CloseWithError(errors.New("HLS init segment changed; decoder reset required"))
				return
			}
			if err := copyHLSSegment(ctx, writer, playlist.initSegment); err != nil {
				_ = writer.CloseWithError(err)
				return
			}
			initURL = playlist.initSegment
		}
		segments := playlist.segments
		if !started && !playlist.end && len(segments) > 3 {
			segments = segments[len(segments)-3:]
		}
		for _, segment := range segments {
			if started && segment.sequence < nextSequence {
				continue
			}
			if err := copyHLSSegment(ctx, writer, segment.url); err != nil {
				// Bytes may already have reached FFmpeg. Replaying a partial
				// segment would corrupt the container; fail the source instead.
				_ = writer.CloseWithError(err)
				return
			}
			started = true
			nextSequence = segment.sequence + 1
		}
		if playlist.end {
			return
		}
		wait := max(250*time.Millisecond, min(playlist.targetDuration/2, 5*time.Second))
		if !waitStreamRetry(ctx, wait) {
			return
		}
	}
}

func fetchHLSPlaylist(ctx context.Context, playlistURL string) (hlsPlaylist, error) {
	ctx, cancel := context.WithTimeout(ctx, radioRequestTimeout)
	defer cancel()
	resp, err := util.Get(ctx, playlistURL, nil)
	if err != nil {
		return hlsPlaylist{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return hlsPlaylist{}, fmt.Errorf("HLS playlist HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return hlsPlaylist{}, err
	}
	if len(data) > 1<<20 {
		return hlsPlaylist{}, errors.New("HLS playlist exceeds size limit")
	}
	return parseHLSPlaylist(resp.Request.URL.String(), string(data))
}

func parseHLSPlaylist(playlistURL, contents string) (hlsPlaylist, error) {
	if !strings.HasPrefix(strings.TrimSpace(contents), "#EXTM3U") {
		return hlsPlaylist{}, errors.New("invalid HLS playlist")
	}
	base, err := url.Parse(playlistURL)
	if err != nil {
		return hlsPlaylist{}, err
	}
	var playlist hlsPlaylist
	var sequence uint64
	pendingSegment := false
	pendingVariant := false
	for _, raw := range strings.Split(contents, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" {
			continue
		}
		if pendingVariant && !strings.HasPrefix(line, "#") {
			playlist.variant = resolveHLSURL(base, line)
			return playlist, nil
		}
		switch {
		case strings.HasPrefix(line, "#EXT-X-KEY:") && hlsAttribute(line, "METHOD") != "NONE":
			return hlsPlaylist{}, errors.New("encrypted HLS is unsupported")
		case strings.HasPrefix(line, "#EXT-X-BYTERANGE:"), line == "#EXT-X-DISCONTINUITY", line == "#EXT-X-GAP":
			return hlsPlaylist{}, errors.New("HLS byte ranges, gaps and discontinuities are unsupported")
		case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
			sequence, err = strconv.ParseUint(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"), 10, 64)
			if err != nil {
				return hlsPlaylist{}, fmt.Errorf("HLS media sequence: %w", err)
			}
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF:"):
			pendingSegment = false
			pendingVariant = true
		case strings.HasPrefix(line, "#EXT-X-TARGETDURATION:"):
			seconds, _ := strconv.ParseFloat(strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:"), 64)
			playlist.targetDuration = time.Duration(seconds * float64(time.Second))
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			if strings.Contains(line, "BYTERANGE=") {
				return hlsPlaylist{}, errors.New("HLS init byte range is unsupported")
			}
			if value := hlsAttribute(line, "URI"); value != "" {
				playlist.initSegment = resolveHLSURL(base, value)
			}
		case strings.HasPrefix(line, "#EXTINF:"):
			pendingSegment = true
		case line == "#EXT-X-ENDLIST":
			playlist.end = true
		case strings.HasPrefix(line, "#"):
			continue
		case pendingSegment:
			playlist.segments = append(playlist.segments, hlsSegment{sequence: sequence, url: resolveHLSURL(base, line)})
			sequence++
			pendingSegment = false
		}
	}
	return playlist, nil
}

func hlsAttribute(line, name string) string {
	_, attributes, _ := strings.Cut(line, ":")
	for attributes != "" {
		key, rest, ok := strings.Cut(strings.TrimSpace(attributes), "=")
		if !ok {
			return ""
		}
		var value string
		if strings.HasPrefix(rest, "\"") {
			value, rest, ok = strings.Cut(rest[1:], "\"")
			if !ok {
				return ""
			}
			attributes = strings.TrimPrefix(strings.TrimSpace(rest), ",")
		} else {
			value, attributes, _ = strings.Cut(rest, ",")
		}
		if key == name {
			return value
		}
	}
	return ""
}

func resolveHLSURL(base *url.URL, value string) string {
	ref, err := url.Parse(value)
	if err != nil {
		return value
	}
	return base.ResolveReference(ref).String()
}

func copyHLSSegment(ctx context.Context, writer *io.PipeWriter, segmentURL string) error {
	resp, err := util.Get(ctx, segmentURL, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HLS segment HTTP %s", resp.Status)
	}
	buffer := make([]byte, 32*1024)
	for {
		count, readErr := resp.Body.Read(buffer)
		if count > 0 {
			if _, err := writer.Write(buffer[:count]); err != nil {
				return err
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

func waitStreamRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func openStreamOnce(ctx context.Context, streamURL string, emit func(string)) (io.ReadCloser, error) {
	headers := make(http.Header)
	headers.Set("Icy-MetaData", "1")
	resp, err := util.Get(ctx, streamURL, headers)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("stream HTTP %s", resp.Status)
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if isHLSURL(resp.Request.URL.String()) || strings.Contains(contentType, "mpegurl") {
		_ = resp.Body.Close()
		return newHLSStream(ctx, resp.Request.URL.String()), nil
	}
	interval, _ := strconv.Atoi(resp.Header.Get("icy-metaint"))
	if interval <= 0 {
		return resp.Body, nil
	}
	return &icyAudioReader{
		body:      resp.Body,
		reader:    bufio.NewReaderSize(resp.Body, 32*1024),
		interval:  interval,
		remaining: interval,
		emit:      emit,
	}, nil
}

type reconnectingStream struct {
	cancel context.CancelFunc
	ctx    context.Context
	url    string
	emit   func(string)

	mu     sync.Mutex
	body   io.ReadCloser
	closed bool
}

func (r *reconnectingStream) Read(buffer []byte) (int, error) {
	for {
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return 0, io.ErrClosedPipe
		}
		body := r.body
		r.mu.Unlock()
		count, err := body.Read(buffer)
		if _, hls := body.(*hlsStream); hls {
			return count, err
		}
		if err == nil {
			return count, nil
		}
		if count > 0 {
			return count, nil
		}
		_ = body.Close()
		r.mu.Lock()
		closed := r.closed
		r.mu.Unlock()
		if closed {
			return 0, io.ErrClosedPipe
		}
		select {
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		case <-time.After(time.Second):
		}
		body, err = openStreamOnce(r.ctx, r.url, r.emit)
		if err != nil {
			if r.ctx.Err() != nil {
				return 0, r.ctx.Err()
			}
			slog.Debug("radio stream reconnect", "error", err)
			continue
		}
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			_ = body.Close()
			return 0, io.ErrClosedPipe
		}
		r.body = body
		r.mu.Unlock()
	}
}

func (r *reconnectingStream) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	if r.cancel != nil {
		r.cancel()
	}
	body := r.body
	r.body = nil
	r.mu.Unlock()
	if body != nil {
		return body.Close()
	}
	return nil
}

type icyAudioReader struct {
	body      io.ReadCloser
	reader    *bufio.Reader
	interval  int
	remaining int
	emit      func(string)
}

func (r *icyAudioReader) Read(buffer []byte) (int, error) {
	if r.interval <= 0 {
		return r.reader.Read(buffer)
	}
	total := 0
	for len(buffer) > 0 {
		if r.remaining == 0 {
			length, err := r.reader.ReadByte()
			if err != nil {
				return total, err
			}
			metadata := make([]byte, int(length)*16)
			if _, err := io.ReadFull(r.reader, metadata); err != nil {
				return total, err
			}
			if r.emit != nil {
				if title := parseICYTitle(strings.TrimRight(string(metadata), "\x00")); title != "" {
					r.emit(title)
				}
			}
			r.remaining = r.interval
		}

		count := min(len(buffer), r.remaining)
		n, err := r.reader.Read(buffer[:count])
		total += n
		r.remaining -= n
		buffer = buffer[n:]
		if err != nil {
			return total, err
		}
		if total > 0 {
			return total, nil
		}
	}
	return total, nil
}

func (r *icyAudioReader) Close() error { return r.body.Close() }

func parseICYTitle(metadata string) string {
	const prefix = "StreamTitle='"
	start := strings.Index(metadata, prefix)
	if start < 0 {
		return ""
	}
	value := metadata[start+len(prefix):]
	for index := 0; index < len(value); index++ {
		if value[index] == '\'' && (index == 0 || value[index-1] != '\\') {
			value = value[:index]
			break
		}
	}
	return strings.TrimSpace(strings.ReplaceAll(value, "\\'", "'"))
}

func cleanStations(stations []Station) []Station {
	result := stations[:0]
	seen := make(map[string]struct{}, len(stations))
	for _, station := range stations {
		station = cleanStation(station)
		if station.StationUUID == "" || station.StreamURL() == "" || station.LastCheckOK == 0 {
			continue
		}
		if _, ok := seen[station.StationUUID]; ok {
			continue
		}
		seen[station.StationUUID] = struct{}{}
		result = append(result, station)
	}
	sortStationsByPopularity(result)
	return result
}

func sortStationsByPopularity(stations []Station) {
	sort.SliceStable(stations, func(i, j int) bool {
		if stations[i].ClickCount != stations[j].ClickCount {
			return stations[i].ClickCount > stations[j].ClickCount
		}
		if stations[i].Votes != stations[j].Votes {
			return stations[i].Votes > stations[j].Votes
		}
		left, right := strings.ToLower(stations[i].DisplayName()), strings.ToLower(stations[j].DisplayName())
		if left != right {
			return left < right
		}
		return stations[i].StationUUID < stations[j].StationUUID
	})
}

func cleanStation(station Station) Station {
	station.StationUUID = strings.TrimSpace(station.StationUUID)
	station.Name = cleanText(station.Name)
	station.URL = cleanText(station.URL)
	station.URLResolved = cleanText(station.URLResolved)
	station.Homepage = cleanText(station.Homepage)
	station.Favicon = cleanText(station.Favicon)
	station.Tags = cleanText(station.Tags)
	station.Country = cleanText(station.Country)
	station.CountryCode = cleanText(station.CountryCode)
	station.Language = cleanText(station.Language)
	station.Codec = cleanText(station.Codec)
	return station
}

func cleanQueueStations(stations []Station) []Station {
	result := make([]Station, 0, len(stations))
	seen := make(map[string]struct{}, len(stations))
	for _, station := range stations {
		station = cleanStation(station)
		if station.StationUUID == "" || station.StreamURL() == "" {
			continue
		}
		if _, ok := seen[station.StationUUID]; ok {
			continue
		}
		seen[station.StationUUID] = struct{}{}
		result = append(result, station)
	}
	return result
}

func cleanText(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if strings.EqualFold(value, "null") {
		return ""
	}
	return value
}

func playlistUUID(streamURL string) string {
	var hash uint64 = 1469598103934665603
	for i := 0; i < len(streamURL); i++ {
		hash ^= uint64(streamURL[i])
		hash *= 1099511628211
	}
	return fmt.Sprintf("local-%x", hash)
}

// ParsePlaylist parses extended M3U and common Winamp PLS files.
func ParsePlaylist(path string) ([]Station, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	switch strings.ToLower(filepath.Ext(path)) {
	case ".m3u", ".m3u8":
		return parseM3U(f, filepath.Dir(path))
	case ".pls":
		return parsePLS(f, filepath.Dir(path))
	default:
		return nil, fmt.Errorf("radio: unsupported playlist %q", filepath.Ext(path))
	}
}

// IsPlaylistPath reports whether path is a supported user radio playlist.
func IsPlaylistPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".m3u", ".m3u8", ".pls":
		return true
	default:
		return false
	}
}

func parseM3U(r io.Reader, baseDir string) ([]Station, error) {
	var result []Station
	scanner := bufio.NewScanner(r)
	name := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#EXTM3U") || strings.HasPrefix(line, "#RADIOBROWSERUUID:") {
			continue
		}
		if strings.HasPrefix(line, "#EXTINF:") {
			if comma := strings.IndexByte(line, ','); comma >= 0 {
				name = strings.TrimSpace(line[comma+1:])
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		result = append(result, playlistStation(name, resolvePlaylistURL(baseDir, line)))
		name = ""
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("radio: read M3U: %w", err)
	}
	return result, nil
}

func parsePLS(r io.Reader, baseDir string) ([]Station, error) {
	files := make(map[int]string)
	titles := make(map[int]string)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(parts[0])
		if strings.HasPrefix(key, "file") {
			if n, err := strconv.Atoi(strings.TrimPrefix(key, "file")); err == nil {
				files[n] = strings.TrimSpace(parts[1])
			}
		} else if strings.HasPrefix(key, "title") {
			if n, err := strconv.Atoi(strings.TrimPrefix(key, "title")); err == nil {
				titles[n] = strings.TrimSpace(parts[1])
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("radio: read PLS: %w", err)
	}
	indices := make([]int, 0, len(files))
	for n := range files {
		indices = append(indices, n)
	}
	sort.Ints(indices)
	result := make([]Station, 0, len(indices))
	for _, n := range indices {
		result = append(result, playlistStation(titles[n], resolvePlaylistURL(baseDir, files[n])))
	}
	return result, nil
}

func resolvePlaylistURL(baseDir, value string) string {
	if strings.Contains(value, "://") || filepath.IsAbs(value) {
		return value
	}
	return filepath.Clean(filepath.Join(baseDir, value))
}

func playlistStation(name, streamURL string) Station {
	return Station{StationUUID: playlistUUID(streamURL), Name: name, URL: streamURL, URLResolved: streamURL}
}
