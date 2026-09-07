package config

import (
	"encoding/json"
	"testing"
)

func TestTrackCacheDefaults(t *testing.T) {
	s := DefaultSettings()
	if s.TrackCache.Retention != CacheThirtyDays || s.TrackCache.MaxBytes != Cache1GB {
		t.Fatalf("track cache defaults = %+v", s.TrackCache)
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var got Settings
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.TrackCache != s.TrackCache {
		t.Fatalf("round trip = %+v, want %+v", got.TrackCache, s.TrackCache)
	}
}

func TestCacheRetentionRejectsUnknownValue(t *testing.T) {
	var retention CacheRetention
	if err := json.Unmarshal([]byte(`"later"`), &retention); err == nil {
		t.Fatal("unknown retention accepted")
	}
}
