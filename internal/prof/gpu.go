package prof

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
)

type gpuReader interface {
	Read() (memKiB, engineNs uint64, ok bool)
}

type fdinfoReader struct {
	paths []string
}

func newGPUReader() gpuReader {
	matches, err := filepath.Glob("/proc/self/fdinfo/*")
	if err != nil || len(matches) == 0 {
		return nil
	}
	var drmPaths []string
	for _, p := range matches {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if bytes.Contains(data, []byte("drm-driver:")) {
			drmPaths = append(drmPaths, p)
		}
	}
	if len(drmPaths) == 0 {
		return nil
	}
	return &fdinfoReader{paths: drmPaths}
}

func (r *fdinfoReader) Read() (memKiB, engineNs uint64, ok bool) {
	for _, p := range r.paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		m := parseFdinfoValue(data, "drm-memory-vram:")
		e := parseFdinfoValue(data, "drm-engine-render:")
		memKiB += m
		engineNs += e
	}
	return memKiB, engineNs, memKiB > 0 || engineNs > 0
}

func parseFdinfoValue(data []byte, prefix string) uint64 {
	for _, line := range bytes.Split(data, []byte("\n")) {
		if !bytes.HasPrefix(line, []byte(prefix)) {
			continue
		}
		parts := bytes.Fields(line)
		if len(parts) < 2 {
			continue
		}
		v, err := strconv.ParseUint(string(parts[1]), 10, 64)
		if err != nil {
			continue
		}
		return v
	}
	return 0
}
