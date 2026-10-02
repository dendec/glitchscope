package app

import (
	"crypto/sha256"
	"path/filepath"
	"testing"

	"github.com/dendec/glitchscope/internal/config"
)

func TestPresetProfileCacheRoundTripAndSourceValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preset-profiles.json")
	key := presetProfileKey{
		name:         "Cream of the Crop/Fractal/example.milk",
		frameRate:    config.FrameRate30,
		targetFPS:    30,
		adaptive:     false,
		width:        1280,
		height:       720,
		ceilingIndex: 2,
	}
	profile := presetProfile{
		digest:   sha256.Sum256([]byte("preset")),
		index:    5,
		floor:    3,
		ready:    true,
		screened: true,
	}
	initial := map[presetProfileKey]cachedPresetProfile{
		key: {key: key, profile: profile, source: "zip:example.milk:12345678:6"},
	}
	if err := writePresetProfileCache(path, initial); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadPresetProfileCache(path, func(string) string { return "zip:example.milk:12345678:6" })
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded[key].profile; got != profileWithRuntimeState(profile) {
		t.Fatalf("loaded profile = %+v, want %+v", got, profileWithRuntimeState(profile))
	}
	if !presetProfileFullyTested(loaded[key].profile, true) {
		t.Fatal("completed profile was not recognized after reload")
	}

	stale, err := loadPresetProfileCache(path, func(string) string { return "zip:example.milk:87654321:6" })
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Fatalf("source-changed profiles were loaded: %v", stale)
	}
}

func TestCompletedPresetProfileSnapshot(t *testing.T) {
	readyKey := presetProfileKey{name: "collection/ready.milk", frameRate: config.FrameRate30, targetFPS: 30}
	heavyKey := presetProfileKey{name: "collection/heavy.milk", frameRate: config.FrameRate30, targetFPS: 30}
	incompleteKey := presetProfileKey{name: "collection/incomplete.milk", frameRate: config.FrameRate30, targetFPS: 30}
	missingKey := presetProfileKey{name: "removed/missing.milk", frameRate: config.FrameRate30, targetFPS: 30}
	profiles := map[presetProfileKey]presetProfile{
		readyKey:      {digest: sha256.Sum256([]byte("ready")), ready: true},
		heavyKey:      {digest: sha256.Sum256([]byte("heavy")), heavy: true},
		incompleteKey: {digest: sha256.Sum256([]byte("incomplete"))},
		missingKey:    {digest: sha256.Sum256([]byte("missing")), ready: true},
	}
	snapshot := completedPresetProfileSnapshot(profiles, func(name string) string {
		if name == missingKey.name {
			return ""
		}
		return "source:" + name
	})
	if len(snapshot) != 2 {
		t.Fatalf("snapshot has %d profiles, want ready and heavy only", len(snapshot))
	}
	if !snapshot[readyKey].profile.ready || !snapshot[heavyKey].profile.heavy {
		t.Fatalf("snapshot did not preserve completed profiles: %+v", snapshot)
	}
}

func profileWithRuntimeState(profile presetProfile) presetProfile {
	profile.stableFrames = 30
	profile.candidate = profile.index
	return profile
}
