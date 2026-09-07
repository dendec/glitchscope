package config

import (
	"encoding/json"
	"fmt"
	"time"
)

// CacheRetention controls how long downloaded catalog tracks are retained.
type CacheRetention int

const (
	CacheDoNotKeep CacheRetention = iota
	CacheOneDay
	CacheSevenDays
	CacheThirtyDays
	CacheNinetyDays
	CacheSixMonths
	CacheForever
)

func AllCacheRetentions() []CacheRetention {
	return []CacheRetention{CacheDoNotKeep, CacheOneDay, CacheSevenDays, CacheThirtyDays, CacheNinetyDays, CacheSixMonths, CacheForever}
}

func (r CacheRetention) String() string {
	switch r {
	case CacheDoNotKeep:
		return "Do not keep"
	case CacheOneDay:
		return "1 day"
	case CacheSevenDays:
		return "7 days"
	case CacheThirtyDays:
		return "30 days"
	case CacheNinetyDays:
		return "90 days"
	case CacheSixMonths:
		return "6 months"
	case CacheForever:
		return "Forever"
	default:
		return "Unknown"
	}
}

func (r CacheRetention) Duration() time.Duration {
	switch r {
	case CacheOneDay:
		return 24 * time.Hour
	case CacheSevenDays:
		return 7 * 24 * time.Hour
	case CacheThirtyDays:
		return 30 * 24 * time.Hour
	case CacheNinetyDays:
		return 90 * 24 * time.Hour
	case CacheSixMonths:
		return 180 * 24 * time.Hour
	default:
		return 0
	}
}

func (r CacheRetention) Validate() error {
	if r < CacheDoNotKeep || r > CacheForever {
		return fmt.Errorf("invalid track cache retention %d", r)
	}
	return nil
}

func (r CacheRetention) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(r.jsonName())
}

func (r *CacheRetention) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("track cache retention: %w", err)
	}
	for _, candidate := range AllCacheRetentions() {
		if candidate.jsonName() == value {
			*r = candidate
			return nil
		}
	}
	return fmt.Errorf("unknown track cache retention %q", value)
}

func (r CacheRetention) jsonName() string {
	return []string{"none", "1_day", "7_days", "30_days", "90_days", "6_months", "forever"}[r]
}

// CacheSizeLimit is a selectable upper bound for downloaded catalog tracks.
type CacheSizeLimit int64

const (
	Cache128MB     CacheSizeLimit = 128 << 20
	Cache256MB     CacheSizeLimit = 256 << 20
	Cache512MB     CacheSizeLimit = 512 << 20
	Cache1GB       CacheSizeLimit = 1 << 30
	Cache2GB       CacheSizeLimit = 2 << 30
	Cache4GB       CacheSizeLimit = 4 << 30
	CacheUnlimited CacheSizeLimit = 0
)

func AllCacheSizeLimits() []CacheSizeLimit {
	return []CacheSizeLimit{Cache128MB, Cache256MB, Cache512MB, Cache1GB, Cache2GB, Cache4GB, CacheUnlimited}
}

func (s CacheSizeLimit) String() string {
	switch s {
	case Cache128MB:
		return "128 MB"
	case Cache256MB:
		return "256 MB"
	case Cache512MB:
		return "512 MB"
	case Cache1GB:
		return "1 GB"
	case Cache2GB:
		return "2 GB"
	case Cache4GB:
		return "4 GB"
	case CacheUnlimited:
		return "Unlimited"
	default:
		return "Unknown"
	}
}

func (s CacheSizeLimit) Validate() error {
	for _, candidate := range AllCacheSizeLimits() {
		if s == candidate {
			return nil
		}
	}
	return fmt.Errorf("invalid track cache size %d", s)
}

// TrackCacheSettings controls downloaded Modland and ModArchive tracks.
type TrackCacheSettings struct {
	Retention CacheRetention `json:"retention"`
	MaxBytes  CacheSizeLimit `json:"max_bytes"`
}

func DefaultTrackCache() TrackCacheSettings {
	return TrackCacheSettings{Retention: CacheThirtyDays, MaxBytes: Cache1GB}
}

func (s TrackCacheSettings) Validate() error {
	if err := s.Retention.Validate(); err != nil {
		return err
	}
	return s.MaxBytes.Validate()
}
