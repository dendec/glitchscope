package ui

import (
	"context"
	"log/slog"

	"github.com/dendec/glitchscope/internal/i18n"
	"github.com/dendec/glitchscope/internal/modarchive"
)

type modArchiveResult struct {
	id    uint64
	url   string
	items []modarchive.DirItem
	err   error
}

func (o *Overlay) beginModArchiveDirectory(target string) {
	if o.modArchivePendingURL == target {
		return
	}
	if o.modArchiveCancel != nil {
		o.modArchiveCancel()
	}
	if o.modArchiveResults == nil {
		o.modArchiveResults = make(chan modArchiveResult, 1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	o.modArchiveCancel = cancel
	o.modArchiveRequestID++
	id := o.modArchiveRequestID
	o.modArchivePendingURL = target
	o.modArchiveWG.Add(1)
	go func() {
		defer o.modArchiveWG.Done()
		defer cancel()
		items, err := modarchive.FetchDirectoryContext(ctx, o.baseDir, target)
		select {
		case o.modArchiveResults <- modArchiveResult{id: id, url: target, items: items, err: err}:
		case <-ctx.Done():
		}
	}()
}

func (o *Overlay) pollModArchiveDirectory() {
	select {
	case result := <-o.modArchiveResults:
		if result.id != o.modArchiveRequestID {
			return
		}
		o.modArchivePendingURL = ""
		if result.err == nil {
			if o.modArchiveItems == nil {
				o.modArchiveItems = make(map[string][]modarchive.DirItem)
			}
			o.modArchiveItems[result.url] = result.items
		} else {
			slog.Warn("modarchive directory failed", "url", result.url, "error", result.err)
		}
		if o.source != sourceModArchive {
			return
		}
		for i := range o.navStack {
			level := &o.navStack[i]
			if level.ctx != ctxCatalog || level.dirPath != result.url {
				continue
			}
			entries := []navEntry{{label: o.catalog.Text(i18n.ValueUnavailable), kind: entryInfo}}
			if result.err == nil {
				entries = o.buildModArchiveEntriesFromItems(result.url, result.items)
			}
			level.entries = withParentEntry(entries)
		}
		o.refreshAlbumLabels()
		o.albumCursor = clampCursor(o.albumCursor, len(o.albumEntries))
		o.syncPanels()
	default:
	}
}
