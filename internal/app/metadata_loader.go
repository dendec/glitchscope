package app

import (
	"context"
	"image"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/dendec/glitchscope/internal/player"
)

type metadataResult struct {
	id    uint64
	infos []player.TrackInfo
	cover *image.RGBA
}

type metadataJob struct {
	ctx   context.Context
	id    uint64
	album player.Album
}

// metadataLoader serializes native reads on a worker. Only immutable snapshots
// cross the boundary; the worker never reads Library, Overlay or GL state.
type metadataLoader struct {
	ctx           context.Context
	cancel        context.CancelFunc
	requestCancel context.CancelFunc
	requestID     atomic.Uint64
	wg            sync.WaitGroup
	jobs          chan metadataJob
	results       chan metadataResult
	load          func(context.Context, player.Album) metadataResult
}

func newMetadataLoader(parent context.Context, load func(context.Context, player.Album) metadataResult) *metadataLoader {
	ctx, cancel := context.WithCancel(parent)
	l := &metadataLoader{ctx: ctx, cancel: cancel, jobs: make(chan metadataJob, 1), results: make(chan metadataResult, 1), load: load}
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case job := <-l.jobs:
				if job.ctx.Err() != nil {
					continue
				}
				result := l.load(job.ctx, job.album)
				result.id = job.id
				if job.ctx.Err() != nil {
					continue
				}
				select {
				case l.results <- result:
				case <-job.ctx.Done():
				}
			}
		}
	}()
	return l
}

func (l *metadataLoader) request(album player.Album) {
	l.invalidate()
	ctx, cancel := context.WithCancel(l.ctx)
	l.requestCancel = cancel
	album.Tracks = slices.Clone(album.Tracks)
	job := metadataJob{ctx: ctx, id: l.requestID.Load(), album: album}
	select {
	case <-l.jobs:
	default:
	}
	l.jobs <- job
}

func (l *metadataLoader) invalidate() {
	if l.requestCancel != nil {
		l.requestCancel()
		l.requestCancel = nil
	}
	l.requestID.Add(1)
}

func (l *metadataLoader) poll() (metadataResult, bool) {
	for {
		select {
		case result := <-l.results:
			if result.id == l.requestID.Load() {
				return result, true
			}
		default:
			return metadataResult{}, false
		}
	}
}

func (l *metadataLoader) close() {
	l.invalidate()
	l.cancel()
	l.wg.Wait()
}

func (a *App) updateMetadata(snapshot *overlayPlaybackSnapshot, albumIdx int, refresh bool) {
	if a.overlay == nil || !a.overlay.UIVisible() {
		if a.albumMetadata != nil {
			a.albumMetadata.invalidate()
		}
		if a.fileMetadata != nil {
			a.fileMetadata.invalidate()
		}
		a.metadataAlbum = -1
		a.metadataFile = ""
		return
	}
	if a.albumMetadata == nil {
		reader := player.NewMetadataReader(baseDir())
		a.albumMetadata = newMetadataLoader(a.appCtx, func(ctx context.Context, album player.Album) metadataResult {
			return metadataResult{infos: reader.Load(ctx, album)}
		})
		fileReader := player.NewMetadataReader(baseDir())
		a.fileMetadata = newMetadataLoader(a.appCtx, func(ctx context.Context, album player.Album) metadataResult {
			infos := fileReader.Load(ctx, album)
			var cover *image.RGBA
			if ctx.Err() == nil && len(album.Tracks) > 0 {
				cover = player.ReadCoverPreview(album.Tracks[0], baseDir())
			}
			return metadataResult{infos: infos, cover: cover}
		})
		a.metadataAlbum = -1
	}
	if albumIdx >= 0 && albumIdx < len(a.lib.Albums) && (refresh || a.metadataAlbum != albumIdx) {
		a.albumMetadata.request(a.lib.Albums[albumIdx])
		a.metadataAlbum = albumIdx
	}
	if result, ok := a.albumMetadata.poll(); ok && a.metadataAlbum == albumIdx {
		snapshot.trackInfos = result.infos
	}
	path := a.overlay.MetadataFilePath()
	if path != a.metadataFile || (refresh && path != "") {
		a.metadataFile = path
		if path == "" {
			a.fileMetadata.invalidate()
		} else {
			a.fileMetadata.request(player.Album{Tracks: []string{path}})
		}
	}
	if result, ok := a.fileMetadata.poll(); ok && len(result.infos) == 1 {
		a.overlay.SetFileMetadata(a.metadataFile, result.infos[0], result.cover)
	}
}
