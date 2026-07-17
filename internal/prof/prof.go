package prof

import (
	"math"
	"sync"
	"time"
)

const (
	procThrottle = 500 * time.Millisecond
	ticksPerSec  = 100
)

type Stats struct {
	MemKB     float64
	CPUPct    float64
	GPUMemKB  float64
	GPUUtilPct float64
	GPUOK     bool
}

type Collector struct {
	mu            sync.Mutex
	lastProcRead  time.Time
	lastStats     Stats

	prevCPUJiffies uint64
	prevCPUWall    time.Time

	gpuReader       gpuReader
	prevGPUEngineNs uint64
	prevGPUWall     time.Time
}

func NewCollector() *Collector {
	return &Collector{
		gpuReader: newGPUReader(),
	}
}

func (c *Collector) ReadStats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if now.Sub(c.lastProcRead) < procThrottle {
		return c.lastStats
	}
	c.lastProcRead = now

	var st Stats

	// Memory.
	if vmRSS, err := readProcStatus(); err == nil {
		st.MemKB = float64(vmRSS)
	}

	// CPU.
	if utime, stime, err := readProcStat(); err == nil {
		jiffies := utime + stime
		if c.prevCPUJiffies > 0 && !c.prevCPUWall.IsZero() {
			dJiff := float64(jiffies - c.prevCPUJiffies)
			dWall := now.Sub(c.prevCPUWall).Seconds()
			if dWall > 0 {
				st.CPUPct = dJiff * 100 / (dWall * ticksPerSec)
				st.CPUPct = math.Round(st.CPUPct)
			}
		}
		c.prevCPUJiffies = jiffies
		c.prevCPUWall = now
	}

	// GPU.
	if c.gpuReader != nil {
		if memKiB, engineNs, ok := c.gpuReader.Read(); ok {
			st.GPUMemKB = float64(memKiB)
			if c.prevGPUEngineNs > 0 && !c.prevGPUWall.IsZero() {
				dNs := float64(engineNs - c.prevGPUEngineNs)
				dWall := now.Sub(c.prevGPUWall).Seconds()
				if dWall > 0 {
					st.GPUUtilPct = dNs / (dWall * 1e9) * 100
					st.GPUUtilPct = math.Round(st.GPUUtilPct)
				}
			}
			c.prevGPUEngineNs = engineNs
			c.prevGPUWall = now
			st.GPUOK = true
		}
	}

	c.lastStats = st
	return st
}
