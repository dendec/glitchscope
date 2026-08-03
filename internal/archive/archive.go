// Package archive reads indexed zstd-compressed archives used by PMV.
package archive

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const (
	magic       = "PMV\x00"
	maxNameSize = 4096
)

var zstdDecoder, zstdDecoderErr = zstd.NewReader(nil)

type Entry struct {
	Name   string
	Length uint32
	offset int64
}

type SourceEntry struct {
	Name string
	Data []byte
}

type Archive struct {
	file    *os.File
	entries map[string]Entry
}

// Open validates an archive header and indexes its compressed entries.
func Open(path string, maxEntries uint32) (*Archive, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	a := &Archive{file: file, entries: make(map[string]Entry)}
	if err := a.readIndex(maxEntries); err != nil {
		_ = file.Close()
		return nil, err
	}
	return a, nil
}

// Write creates a version 1 archive with independently zstd-compressed entries.
func Write(path string, entries []SourceEntry) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		return err
	}
	defer func() { _ = encoder.Close() }()

	type compressedEntry struct {
		name string
		data []byte
	}
	compressed := make([]compressedEntry, len(entries))
	for i, entry := range entries {
		if len(entry.Name) == 0 || len([]byte(entry.Name)) > 65535 {
			return fmt.Errorf("archive path is too long or empty: %q", entry.Name)
		}
		compressed[i] = compressedEntry{name: filepath.ToSlash(entry.Name), data: encoder.EncodeAll(entry.Data, nil)}
	}

	write := func(value any) error {
		return binary.Write(file, binary.LittleEndian, value)
	}
	if _, err := file.WriteString(magic); err != nil {
		return err
	}
	if err := write(uint32(1)); err != nil {
		return err
	}
	if err := write(uint32(len(compressed))); err != nil {
		return err
	}
	for _, entry := range compressed {
		name := []byte(entry.name)
		if err := write(uint16(len(name))); err != nil {
			return err
		}
		if _, err := file.Write(name); err != nil {
			return err
		}
		if err := write(uint32(len(entry.data))); err != nil {
			return err
		}
	}
	for _, entry := range compressed {
		if _, err := file.Write(entry.data); err != nil {
			return err
		}
	}
	return file.Sync()
}

func (a *Archive) readIndex(maxEntries uint32) error {
	fileMagic := make([]byte, len(magic))
	if _, err := io.ReadFull(a.file, fileMagic); err != nil {
		return err
	}
	if string(fileMagic) != magic {
		return fmt.Errorf("bad archive magic: %q", fileMagic)
	}

	var version uint32
	if err := binary.Read(a.file, binary.LittleEndian, &version); err != nil {
		return err
	}
	if version != 1 {
		return fmt.Errorf("unsupported archive version: %d", version)
	}

	var count uint32
	if err := binary.Read(a.file, binary.LittleEndian, &count); err != nil {
		return err
	}
	if count > maxEntries {
		return fmt.Errorf("too many archive entries: %d", count)
	}

	type rawEntry struct {
		name   string
		length uint32
	}
	raw := make([]rawEntry, count)
	for i := range raw {
		var nameLength uint16
		if err := binary.Read(a.file, binary.LittleEndian, &nameLength); err != nil {
			return err
		}
		if nameLength == 0 || int(nameLength) > maxNameSize {
			return fmt.Errorf("invalid archive name length: %d", nameLength)
		}
		nameBytes := make([]byte, nameLength)
		if _, err := io.ReadFull(a.file, nameBytes); err != nil {
			return err
		}
		name := filepath.ToSlash(string(nameBytes))
		if filepath.IsAbs(name) || name == "." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
			return fmt.Errorf("unsafe archive name: %q", name)
		}
		if err := binary.Read(a.file, binary.LittleEndian, &raw[i].length); err != nil {
			return err
		}
		raw[i].name = name
	}

	offset, err := a.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	for _, item := range raw {
		if _, exists := a.entries[item.name]; exists {
			return fmt.Errorf("duplicate archive entry: %q", item.name)
		}
		a.entries[item.name] = Entry{Name: item.name, Length: item.length, offset: offset}
		offset += int64(item.length)
	}
	return nil
}

// Entries returns all archive entries in name order.
func (a *Archive) Entries() []Entry {
	entries := make([]Entry, 0, len(a.entries))
	for _, entry := range a.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

// Read returns the decompressed bytes for an archive entry.
func (a *Archive) Read(name string) ([]byte, error) {
	entry, ok := a.entries[name]
	if !ok {
		return nil, fmt.Errorf("archive entry %q not found", name)
	}
	compressed := make([]byte, entry.Length)
	if _, err := a.file.ReadAt(compressed, entry.offset); err != nil {
		return nil, err
	}
	data, err := zstdDecompress(compressed)
	if err != nil {
		return nil, fmt.Errorf("decode %q: %w", name, err)
	}
	return data, nil
}

// Extract writes all archive entries to dir and returns the number of files.
func (a *Archive) Extract(dir string) (int, error) {
	n := 0
	for _, entry := range a.Entries() {
		data, err := a.Read(entry.Name)
		if err != nil {
			return n, err
		}
		path := filepath.Join(dir, filepath.FromSlash(entry.Name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return n, err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func zstdDecompress(data []byte) ([]byte, error) {
	if zstdDecoderErr != nil {
		return nil, zstdDecoderErr
	}
	return zstdDecoder.DecodeAll(data, nil)
}

func (a *Archive) Close() error {
	if a == nil || a.file == nil {
		return nil
	}
	return a.file.Close()
}
