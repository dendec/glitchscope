//go:build windows

package ui

import (
	"syscall"
	"unsafe"
)

var (
	deviceKernel32             = syscall.NewLazyDLL("kernel32.dll")
	deviceGlobalMemoryStatusEx = deviceKernel32.NewProc("GlobalMemoryStatusEx")
)

type deviceMemoryStatusEx struct {
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

func readTotalRAMPlatform() string {
	status := deviceMemoryStatusEx{Length: uint32(unsafe.Sizeof(deviceMemoryStatusEx{}))}
	result, _, _ := deviceGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if result == 0 {
		return ""
	}
	bytes := status.TotalPhys
	switch {
	case bytes >= 1<<30:
		return formatFloat(float64(bytes)/float64(1<<30)) + " GB"
	case bytes >= 1<<20:
		return formatFloat(float64(bytes)/float64(1<<20)) + " MB"
	default:
		return formatFloat(float64(bytes)/float64(1<<10)) + " KB"
	}
}
