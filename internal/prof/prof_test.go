package prof

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseMemoryKB(t *testing.T) {
	got, err := parseMemoryKB([]byte("MemTotal: 1024 kB\nMemAvailable: 768 kB\n"), "MemAvailable:")
	if err != nil || got != 768 {
		t.Fatalf("parseMemoryKB = %d, %v", got, err)
	}
	if _, err := parseMemoryKB([]byte("MemTotal: 1024 kB\n"), "MemAvailable:"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing field error = %v", err)
	}
}

func TestProcStatus(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("procfs only on linux")
	}
	vmRSS, err := readProcStatus()
	if err != nil {
		t.Fatalf("readProcStatus: %v", err)
	}
	if vmRSS <= 0 {
		t.Fatalf("VmRSS should be > 0, got %d", vmRSS)
	}
}

func TestProcStat(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("procfs only on linux")
	}
	_, _, err := readProcStat()
	if err != nil {
		t.Fatalf("readProcStat: %v", err)
	}
}

func TestReadStats(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("procfs only on linux")
	}
	c := NewCollector()
	_ = c.ReadStats()

	time.Sleep(600 * time.Millisecond)

	s2 := c.ReadStats()

	if s2.MemKB <= 0 {
		t.Fatalf("MemKB should be > 0, got %.0f", s2.MemKB)
	}
}
