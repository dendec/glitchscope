package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dendec/glitchscope/internal/config"
	"github.com/dendec/glitchscope/internal/presets"
)

const presetProfileCacheVersion = 1

type cachedPresetProfile struct {
	key     presetProfileKey
	profile presetProfile
	source  string
}

type presetProfileCacheFile struct {
	Version  int                   `json:"version"`
	Profiles []presetProfileRecord `json:"profiles"`
}

type presetProfileRecord struct {
	Name         string           `json:"name"`
	FrameRate    config.FrameRate `json:"frame_rate"`
	TargetFPS    int32            `json:"target_fps"`
	Adaptive     bool             `json:"adaptive"`
	Width        int              `json:"width"`
	Height       int              `json:"height"`
	CeilingIndex int              `json:"ceiling_index"`
	Source       string           `json:"source"`
	Digest       string           `json:"digest"`
	Index        int              `json:"index"`
	Floor        int              `json:"floor"`
	Ready        bool             `json:"ready"`
	Screened     bool             `json:"screened"`
	Heavy        bool             `json:"heavy"`
}

func presetProfileCachePath(settingsPath string) string {
	return filepath.Join(filepath.Dir(settingsPath), "preset-profiles.json")
}

func loadPresetProfileCache(path string, sourceFor func(string) string) (map[presetProfileKey]cachedPresetProfile, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open preset profile cache: %w", err)
	}
	defer f.Close()

	decoder := json.NewDecoder(f)
	var cache presetProfileCacheFile
	if err := decoder.Decode(&cache); err != nil {
		return nil, fmt.Errorf("decode preset profile cache: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("unexpected trailing JSON value")
		}
		return nil, fmt.Errorf("decode preset profile cache trailer: %w", err)
	}
	if cache.Version != presetProfileCacheVersion {
		return nil, fmt.Errorf("unsupported preset profile cache version %d", cache.Version)
	}

	profiles := make(map[presetProfileKey]cachedPresetProfile, min(len(cache.Profiles), maxPresetProfiles))
	for _, record := range cache.Profiles {
		if len(profiles) >= maxPresetProfiles || record.Source == "" || record.Source != sourceFor(record.Name) ||
			record.Name == "" || record.TargetFPS <= 0 || record.Width <= 0 || record.Height <= 0 ||
			record.CeilingIndex < 0 || record.Index < 0 || record.Floor < 0 || !record.Ready && !record.Heavy {
			continue
		}
		if record.FrameRate.Validate() != nil {
			continue
		}
		digest, err := hex.DecodeString(record.Digest)
		if err != nil || len(digest) != sha256.Size {
			continue
		}
		var sum [sha256.Size]byte
		copy(sum[:], digest)
		key := presetProfileKey{
			name:         record.Name,
			frameRate:    record.FrameRate,
			targetFPS:    record.TargetFPS,
			adaptive:     record.Adaptive,
			width:        record.Width,
			height:       record.Height,
			ceilingIndex: record.CeilingIndex,
		}
		profile := presetProfile{
			digest:   sum,
			index:    record.Index,
			floor:    record.Floor,
			ready:    record.Ready,
			screened: record.Screened,
			heavy:    record.Heavy,
		}
		if profile.ready {
			profile.stableFrames = 30
			profile.candidate = profile.index
		}
		profiles[key] = cachedPresetProfile{key: key, profile: profile, source: record.Source}
	}
	return profiles, nil
}

func writePresetProfileCache(path string, profiles map[presetProfileKey]cachedPresetProfile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	keys := make([]presetProfileKey, 0, len(profiles))
	for key, profile := range profiles {
		if profile.profile.ready || profile.profile.heavy {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].name != keys[j].name {
			return keys[i].name < keys[j].name
		}
		if keys[i].frameRate != keys[j].frameRate {
			return keys[i].frameRate < keys[j].frameRate
		}
		if keys[i].targetFPS != keys[j].targetFPS {
			return keys[i].targetFPS < keys[j].targetFPS
		}
		if keys[i].adaptive != keys[j].adaptive {
			return !keys[i].adaptive
		}
		if keys[i].width != keys[j].width {
			return keys[i].width < keys[j].width
		}
		if keys[i].height != keys[j].height {
			return keys[i].height < keys[j].height
		}
		return keys[i].ceilingIndex < keys[j].ceilingIndex
	})
	if len(keys) > maxPresetProfiles {
		keys = keys[len(keys)-maxPresetProfiles:]
	}
	cache := presetProfileCacheFile{Version: presetProfileCacheVersion, Profiles: make([]presetProfileRecord, 0, len(keys))}
	for _, key := range keys {
		cached := profiles[key]
		cache.Profiles = append(cache.Profiles, presetProfileRecord{
			Name:         key.name,
			FrameRate:    key.frameRate,
			TargetFPS:    key.targetFPS,
			Adaptive:     key.adaptive,
			Width:        key.width,
			Height:       key.height,
			CeilingIndex: key.ceilingIndex,
			Source:       cached.source,
			Digest:       hex.EncodeToString(cached.profile.digest[:]),
			Index:        cached.profile.index,
			Floor:        cached.profile.floor,
			Ready:        cached.profile.ready,
			Screened:     cached.profile.screened,
			Heavy:        cached.profile.heavy,
		})
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".preset-profiles-*.tmp")
	if err != nil {
		return fmt.Errorf("create cache temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if err := json.NewEncoder(tmp).Encode(cache); err != nil {
		cleanup()
		return fmt.Errorf("encode cache: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync cache: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close cache: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("publish cache: %w", err)
	}
	return nil
}

func (a *App) presetSourceFingerprint(name string) string {
	fingerprint, err := presets.SourceFingerprint(name)
	if err != nil {
		return ""
	}
	return fingerprint
}

func (a *App) presetProfileCacheLoaded(path string) {
	loaded, err := loadPresetProfileCache(path, a.presetSourceFingerprint)
	if err != nil {
		slog.Warn("preset profile cache load failed", "path", path, "error", err)
		loaded = nil
	}
	if len(loaded) > 0 {
		a.presetTuning.profiles = make(map[presetProfileKey]presetProfile, len(loaded))
		for key, cached := range loaded {
			a.presetTuning.profiles[key] = cached.profile
		}
	}
}

func completedPresetProfileSnapshot(profiles map[presetProfileKey]presetProfile, sourceFor func(string) string) map[presetProfileKey]cachedPresetProfile {
	snapshot := make(map[presetProfileKey]cachedPresetProfile, len(profiles))
	for key, profile := range profiles {
		if !profile.ready && !profile.heavy {
			continue
		}
		source := sourceFor(key.name)
		if source == "" {
			continue
		}
		snapshot[key] = cachedPresetProfile{key: key, profile: profile, source: source}
	}
	return snapshot
}

func (a *App) savePresetProfileCache(path string) {
	profiles := completedPresetProfileSnapshot(a.presetTuning.profiles, a.presetSourceFingerprint)
	if err := writePresetProfileCache(path, profiles); err != nil {
		slog.Warn("preset profile cache save failed", "path", path, "error", err)
	}
}

func (a *App) invalidatePresetProfiles(prefix string) {
	for key := range a.presetTuning.profiles {
		if !strings.HasPrefix(key.name, prefix) {
			continue
		}
		delete(a.presetTuning.profiles, key)
	}
}
