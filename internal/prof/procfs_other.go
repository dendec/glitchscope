//go:build !linux && !windows

package prof

import "errors"

const cpuTicksPerSecond = 1

func readProcStatus() (int64, error) { return 0, errors.New("process memory statistics unavailable") }

func PeakMemoryKB() (int64, error) { return 0, errors.New("peak memory statistics unavailable") }

func AvailableMemoryKB() (int64, error) {
	return 0, errors.New("available memory statistics unavailable")
}

func readProcStat() (uint64, uint64, error) {
	return 0, 0, errors.New("process CPU statistics unavailable")
}
