package prof

import (
	"bytes"
	"fmt"
	"strconv"
)

func parseMemoryKB(data []byte, field string) (int64, error) {
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
