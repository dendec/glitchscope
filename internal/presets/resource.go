package presets

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf16"
)

const maxResourceBytes = 64 << 20

// copyResourcePresets reads the Win32 RES record format, not PE executables.
// Only named TEXT/MILK<n> payloads are exported; bytes stay unchanged in the ZIP.
func copyResourcePresets(ctx context.Context, writer *zip.Writer, source string) error {
	file, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open preset resources: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxResourceBytes+1))
	if err != nil {
		return fmt.Errorf("read preset resources: %w", err)
	}
	if len(data) > maxResourceBytes {
		return fmt.Errorf("preset resources exceed %d bytes", maxResourceBytes)
	}
	// Every Win32 RES starts with an empty resource with ordinal type/name zero.
	signature := []byte{0, 0, 0, 0, 32, 0, 0, 0, 255, 255, 0, 0, 255, 255, 0, 0}
	if len(data) < 32 || !bytes.Equal(data[:16], signature) {
		return fmt.Errorf("invalid Win32 preset resource signature")
	}
	seen := make(map[uint64]bool)
	for offset, records := 32, 0; offset < len(data); records++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if records >= maxPackEntries || len(data)-offset < 8 {
			return fmt.Errorf("invalid preset resource record count or truncated header")
		}
		size := uint64(binary.LittleEndian.Uint32(data[offset:]))
		header := uint64(binary.LittleEndian.Uint32(data[offset+4:]))
		remaining := uint64(len(data) - offset)
		if header < 32 || header%4 != 0 || header > remaining || size > remaining-header {
			return fmt.Errorf("invalid preset resource bounds at %d", offset)
		}
		end := offset + int(header)
		typ, cursor, err := resourceIdentifier(data, offset+8, end)
		if err != nil {
			return err
		}
		name, cursor, err := resourceIdentifier(data, cursor, end)
		if err != nil {
			return err
		}
		if (cursor+3)&^3 > end-16 {
			return fmt.Errorf("truncated preset resource metadata")
		}
		if typ == "TEXT" && strings.HasPrefix(name, "MILK") {
			number, err := strconv.ParseUint(strings.TrimPrefix(name, "MILK"), 10, 32)
			if err != nil || seen[number] {
				return fmt.Errorf("invalid or duplicate preset resource %q", name)
			}
			payload := data[end : end+int(size)]
			if size > maxPresetBytes || !bytes.Contains(payload, []byte("[preset00]")) || bytes.IndexByte(payload, 0) >= 0 {
				return fmt.Errorf("invalid MilkDrop payload in resource %q", name)
			}
			entry, err := writer.Create(fmt.Sprintf("Presets/MilkDrop2077.R%03d.milk", number))
			if err != nil {
				return fmt.Errorf("create resource preset: %w", err)
			}
			if _, err := entry.Write(payload); err != nil {
				return fmt.Errorf("write resource preset: %w", err)
			}
			seen[number] = true
		}
		offset = (end + int(size) + 3) &^ 3
		if offset > len(data) {
			return fmt.Errorf("truncated preset resource padding")
		}
	}
	if len(seen) == 0 {
		return fmt.Errorf("resource file contains no MilkDrop presets")
	}
	notice, err := writer.Create("Sources/Presets/MilkDrop2077.txt")
	if err != nil {
		return err
	}
	_, err = io.WriteString(notice, "MilkDrop2077 preset resources, extracted without modification.\nSource: https://github.com/milkdrop2077/milkdrop2077\nResource: https://github.com/milkdrop2077/milkdrop2077/blob/main/PRESETS.RES\nLicense: https://github.com/milkdrop2077/milkdrop2077/blob/main/LICENSE\n")
	return err
}

// Ordinal identifiers are represented as empty strings; they cannot match the
// named TEXT and MILK resources used by this collection.
func resourceIdentifier(data []byte, offset, end int) (string, int, error) {
	if offset+2 > end {
		return "", offset, fmt.Errorf("truncated preset resource identifier")
	}
	if binary.LittleEndian.Uint16(data[offset:]) == 0xffff {
		if offset+4 > end {
			return "", offset, fmt.Errorf("truncated ordinal resource identifier")
		}
		return "", offset + 4, nil
	}
	var chars []uint16
	for offset+2 <= end {
		value := binary.LittleEndian.Uint16(data[offset:])
		offset += 2
		if value == 0 {
			return string(utf16.Decode(chars)), offset, nil
		}
		chars = append(chars, value)
	}
	return "", offset, fmt.Errorf("unterminated preset resource identifier")
}
