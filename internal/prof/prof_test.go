package prof

import (
	"runtime"
	"testing"
	"time"
)

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
