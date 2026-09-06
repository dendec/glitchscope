package prof

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	procStatus = "/proc/self/status"
	procStat   = "/proc/self/stat"
)

func readProcStatus() (vmRSS int64, err error) {
	return readProcMemory("VmRSS:")
}

// PeakMemoryKB reads the kernel process high-water RSS, including native allocations.
func PeakMemoryKB() (int64, error) { return readProcMemory("VmHWM:") }

func readProcMemory(field string) (int64, error) {
	data, err := os.ReadFile(procStatus)
	if err != nil {
		return 0, err
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		if !bytes.HasPrefix(line, []byte(field)) {
			continue
		}
		parts := bytes.Fields(line)
		if len(parts) < 2 {
			continue
		}
		v, err := strconv.ParseInt(string(parts[1]), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse %s: %w", field, err)
		}
		return v, nil
	}
	return 0, fmt.Errorf("%s not found", field)
}

func readProcStat() (utime, stime uint64, err error) {
	data, err := os.ReadFile(procStat)
	if err != nil {
		return 0, 0, err
	}
	s := string(data)
	rparen := strings.LastIndex(s, ")")
	if rparen < 0 {
		return 0, 0, fmt.Errorf("no closing paren in stat")
	}
	after := s[rparen+2:]
	fields := strings.Fields(after)
	if len(fields) < 13 {
		return 0, 0, fmt.Errorf("too few fields in stat")
	}
	utime, err = strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse utime: %w", err)
	}
	stime, err = strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse stime: %w", err)
	}
	return utime, stime, nil
}
