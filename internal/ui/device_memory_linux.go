//go:build linux

package ui

import "syscall"

func readTotalRAMPlatform() string {
	var info syscall.Sysinfo_t
	if err := syscall.Sysinfo(&info); err != nil {
		return ""
	}
	bytes := uint64(info.Totalram) * uint64(info.Unit)
	switch {
	case bytes >= 1<<30:
		return formatFloat(float64(bytes)/float64(1<<30)) + " GB"
	case bytes >= 1<<20:
		return formatFloat(float64(bytes)/float64(1<<20)) + " MB"
	default:
		return formatFloat(float64(bytes)/float64(1<<10)) + " KB"
	}
}
