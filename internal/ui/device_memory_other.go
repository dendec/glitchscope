//go:build !linux && !windows

package ui

func readTotalRAMPlatform() string { return "" }
