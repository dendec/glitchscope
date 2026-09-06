package ui

import (
	"errors"
	"strings"
	"testing"
)

func TestHandheldInfoPrefersPortMasterEnvironment(t *testing.T) {
	values := map[string]string{
		"DEVICE_NAME": "TrimUI Smart Pro",
		"DEVICE_CPU":  "A133P",
		"CFW_NAME":    "TrimUI",
		"CFW_VERSION": "1.0.4",
		"PM_VERSION":  "2026.08",
	}
	d := &DeviceInfo{}
	d.fillHandheldInfo(func(key string) string { return values[key] }, func(string) ([]byte, error) {
		return nil, errors.New("unexpected read")
	})
	if d.DeviceName != "TrimUI Smart Pro" || d.SoC != "A133P" || d.CFWName != "TrimUI" || d.PMVersion != "2026.08" {
		t.Fatalf("environment info = %+v", d)
	}
}

func TestHandheldInfoFallsBackToSystemFiles(t *testing.T) {
	files := map[string]string{
		"/etc/os-release":                  "OS_NAME=muOS\nOS_VERSION=2502\n",
		"/tmp/device-home/.config/.DEVICE": "RG35XX H\n",
		"/sys/devices/soc0/soc_id":         "H700\x00",
		"/roms/ports/PortMaster/version":   "2026.07\n",
	}
	readFile := func(path string) ([]byte, error) {
		if value, ok := files[path]; ok {
			return []byte(value), nil
		}
		return nil, errors.New("missing")
	}
	d := &DeviceInfo{}
	d.fillHandheldInfo(func(key string) string {
		if key == "HOME" {
			return "/tmp/device-home"
		}
		return ""
	}, readFile)
	if d.DeviceName != "RG35XX H" || d.SoC != "H700" || d.CFWName != "muOS" || d.CFWVersion != "2502" || d.PMVersion != "2026.07" {
		t.Fatalf("fallback info = %+v", d)
	}
}

func TestDeviceInfoProviderIsLazyAndCached(t *testing.T) {
	calls := 0
	o := &Overlay{}
	o.SetDeviceInfoProvider(func() *DeviceInfo {
		calls++
		return &DeviceInfo{DeviceName: "Test Handheld"}
	})
	if calls != 0 {
		t.Fatal("provider called before Device was opened")
	}
	if text := strings.Join(o.deviceInfoLines(), "\n"); !strings.Contains(text, "Test Handheld") {
		t.Fatalf("device name missing from %q", text)
	}
	_ = o.deviceInfoLines()
	if calls != 1 {
		t.Fatalf("provider called %d times, want once", calls)
	}
}
