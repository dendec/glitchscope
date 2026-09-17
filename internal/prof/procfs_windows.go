//go:build windows

package prof

import (
	"fmt"
	"syscall"
	"unsafe"
)

const cpuTicksPerSecond = 10_000_000

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	psapi                    = syscall.NewLazyDLL("psapi.dll")
	getCurrentProcessProc    = kernel32.NewProc("GetCurrentProcess")
	getProcessTimesProc      = kernel32.NewProc("GetProcessTimes")
	globalMemoryStatusExProc = kernel32.NewProc("GlobalMemoryStatusEx")
	getProcessMemoryInfoProc = psapi.NewProc("GetProcessMemoryInfo")
)

type winFileTime struct {
	LowDateTime  uint32
	HighDateTime uint32
}

type processMemoryCounters struct {
	Cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func currentProcess() (uintptr, error) {
	handle, _, _ := getCurrentProcessProc.Call()
	if handle == 0 {
		return 0, fmt.Errorf("GetCurrentProcess failed")
	}
	return handle, nil
}

func readProcStatus() (int64, error) {
	handle, err := currentProcess()
	if err != nil {
		return 0, err
	}
	counters := processMemoryCounters{Cb: uint32(unsafe.Sizeof(processMemoryCounters{}))}
	result, _, callErr := getProcessMemoryInfoProc.Call(
		handle,
		uintptr(unsafe.Pointer(&counters)),
		uintptr(counters.Cb),
	)
	if result == 0 {
		return 0, fmt.Errorf("GetProcessMemoryInfo: %w", callErr)
	}
	return int64(counters.WorkingSetSize / 1024), nil
}

// PeakMemoryKB reads the process working-set high-water mark.
func PeakMemoryKB() (int64, error) {
	handle, err := currentProcess()
	if err != nil {
		return 0, err
	}
	counters := processMemoryCounters{Cb: uint32(unsafe.Sizeof(processMemoryCounters{}))}
	result, _, callErr := getProcessMemoryInfoProc.Call(
		handle,
		uintptr(unsafe.Pointer(&counters)),
		uintptr(counters.Cb),
	)
	if result == 0 {
		return 0, fmt.Errorf("GetProcessMemoryInfo: %w", callErr)
	}
	return int64(counters.PeakWorkingSetSize / 1024), nil
}

// AvailableMemoryKB reads the physical memory available to applications.
func AvailableMemoryKB() (int64, error) {
	status := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	result, _, callErr := globalMemoryStatusExProc.Call(uintptr(unsafe.Pointer(&status)))
	if result == 0 {
		return 0, fmt.Errorf("GlobalMemoryStatusEx: %w", callErr)
	}
	return int64(status.AvailPhys / 1024), nil
}

func readProcStat() (utime, stime uint64, err error) {
	handle, err := currentProcess()
	if err != nil {
		return 0, 0, err
	}
	var creation, exit, kernel, user winFileTime
	result, _, callErr := getProcessTimesProc.Call(
		handle,
		uintptr(unsafe.Pointer(&creation)),
		uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if result == 0 {
		return 0, 0, fmt.Errorf("GetProcessTimes: %w", callErr)
	}
	return fileTimeValue(user), fileTimeValue(kernel), nil
}

func fileTimeValue(value winFileTime) uint64 {
	return uint64(value.HighDateTime)<<32 | uint64(value.LowDateTime)
}
