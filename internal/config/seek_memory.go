package config

import (
	"encoding/json"
	"fmt"
)

// SeekMemory controls the working PCM budget for exact tracker seeking.
type SeekMemory int

const (
	SeekMemoryFull SeekMemory = iota
	SeekMemoryLow
	SeekMemoryStreaming
)

func AllSeekMemoryModes() []SeekMemory {
	return []SeekMemory{SeekMemoryFull, SeekMemoryLow, SeekMemoryStreaming}
}

func (m SeekMemory) String() string {
	switch m {
	case SeekMemoryFull:
		return "Exact (256 MiB)"
	case SeekMemoryLow:
		return "Low memory (128 MiB)"
	case SeekMemoryStreaming:
		return "Streaming"
	default:
		return "Unknown"
	}
}

func (m SeekMemory) BudgetBytes() int64 {
	switch m {
	case SeekMemoryFull:
		return 256 << 20
	case SeekMemoryLow:
		return 128 << 20
	default:
		return 0
	}
}

func (m SeekMemory) Validate() error {
	if m < SeekMemoryFull || m > SeekMemoryStreaming {
		return fmt.Errorf("invalid seek memory mode %d", m)
	}
	return nil
}

func (m SeekMemory) MarshalJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal([]string{"exact", "low_memory", "streaming"}[m])
}

func (m *SeekMemory) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch value {
	case "exact":
		*m = SeekMemoryFull
	case "low_memory":
		*m = SeekMemoryLow
	case "streaming":
		*m = SeekMemoryStreaming
	default:
		return fmt.Errorf("unknown seek memory mode %q", value)
	}
	return nil
}
