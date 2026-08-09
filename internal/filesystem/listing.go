// Package filesystem contains the shared, deterministic filesystem traversal.
package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/dendec/pmv/internal/formats"
)

type Status int

const (
	StatusOK Status = iota
	StatusPartial
	StatusFailed
)

func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusPartial:
		return "partial"
	case StatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

type Entry struct {
	Path string
	Name string
	Info fs.DirEntry
}

func (e Entry) IsDir() bool     { return e.Info.IsDir() }
func (e Entry) IsRegular() bool { return e.Info.Type().IsRegular() }
func (e Entry) IsSymlink() bool { return e.Info.Type()&os.ModeSymlink != 0 }

type Issue struct {
	Path string
	Err  error
}

func (i Issue) Error() string {
	return fmt.Sprintf("%s: %v", i.Path, i.Err)
}

type Report struct {
	Status Status
	Issues []Issue
}

func (r Report) Err() error {
	if len(r.Issues) == 0 {
		return nil
	}
	errs := make([]error, 0, len(r.Issues))
	for _, issue := range r.Issues {
		errs = append(errs, issue)
	}
	return errors.Join(errs...)
}

type Options struct {
	Include func(Entry) bool
	Descend func(Entry) bool
}

func IsAudioFile(entry Entry) bool {
	if entry.IsSymlink() || !entry.IsRegular() || len(entry.Name) == 0 || entry.Name[0] == '.' {
		return false
	}
	ext := filepath.Ext(entry.Name)
	return formats.IsSupportedExt(ext)
}

// Walk visits entries in lexical order and records inaccessible subtrees.
// Policy filtering is not an error: errors only describe failed filesystem IO.
func Walk(ctx context.Context, root string, options Options, visit func(Entry)) Report {
	report := Report{Status: StatusOK}
	if err := walkDir(ctx, root, options, visit, &report); err != nil {
		report.Status = StatusFailed
		report.Issues = append(report.Issues, Issue{Path: root, Err: err})
	}
	return report
}

func walkDir(ctx context.Context, dir string, options Options, visit func(Entry), report *Report) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, info := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry := Entry{Path: filepath.Join(dir, info.Name()), Name: info.Name(), Info: info}
		if options.Include == nil || options.Include(entry) {
			visit(entry)
		}
		if !entry.IsDir() || entry.IsSymlink() || (options.Descend != nil && !options.Descend(entry)) {
			continue
		}
		if err := walkDir(ctx, entry.Path, options, visit, report); err != nil {
			report.Status = StatusPartial
			report.Issues = append(report.Issues, Issue{Path: entry.Path, Err: err})
		}
	}
	return nil
}
