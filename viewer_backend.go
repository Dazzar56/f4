package main

import (
	"context"
	"io"
	"sync"

	"github.com/unxed/f4/piecetable"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

// ViewerBackend provides async random access to a file using small cache window.
type ViewerBackend struct {
	file vfs.ReadAtCloser
	size int64

	path    string
	indexer vfs.LineIndexer

	// totalLines is what the far side last reported, and totalForSize is the
	// file size it reported it for. A log file grows while it is being read,
	// so the total is only reused while the size it was counted at still
	// holds; anything else would put the viewer at the wrong offset.
	totalLines   int64
	totalForSize int64

	mu         sync.Mutex
	cacheOff   int64
	cacheData  []byte
	isFetching bool

	ctx       context.Context
	cancelCtx context.CancelFunc
}

func NewViewerBackend(ctx context.Context, v vfs.VFS, path string) (*ViewerBackend, error) {
	f, err := v.Open(ctx, path)
	if err != nil {
		return nil, err
	}

	bCtx, bCancel := context.WithCancel(context.Background())
	b := &ViewerBackend{
		file:         f,
		size:         f.Size(),
		path:         path,
		totalLines:   -1,
		totalForSize: -1,
		ctx:          bCtx,
		cancelCtx:    bCancel,
	}
	if indexer, ok := v.(vfs.LineIndexer); ok {
		b.indexer = indexer
	}
	return b, nil
}

func (b *ViewerBackend) Close() error {
	if b.cancelCtx != nil {
		b.cancelCtx()
	}
	return b.file.Close()
}

func (b *ViewerBackend) Size() int64 {
	if b.file != nil {
		newSize := b.file.Size()
		b.mu.Lock()
		b.size = newSize
		b.mu.Unlock()
	}
	return b.size
}

func (b *ViewerBackend) ReadAt(offset int64, length int) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if offset >= b.size {
		return nil, io.EOF
	}
	if offset+int64(length) > b.size {
		length = int(b.size - offset)
	}

	// Check cache hit
	if b.cacheData != nil && offset >= b.cacheOff && (offset+int64(length)) <= (b.cacheOff+int64(len(b.cacheData))) {
		start := offset - b.cacheOff
		return b.cacheData[start : start+int64(length)], nil
	}

	// Cache miss -> Trigger fetch in background
	if !b.isFetching {
		b.isFetching = true

		fetchOff := offset - 64*1024
		if fetchOff < 0 {
			fetchOff = 0
		}
		fetchLen := 256 * 1024 // We only keep 256KB in memory
		if fetchOff+int64(fetchLen) > b.size {
			fetchLen = int(b.size - fetchOff)
		}

		go func() {
			buf := make([]byte, fetchLen)
			n, err := b.file.ReadAt(b.ctx, buf, fetchOff)

			vtui.FrameManager.PostTask(func() {
				b.mu.Lock()
				if b.ctx.Err() == nil {
					if err == nil || err == io.EOF {
						b.cacheOff = fetchOff
						b.cacheData = buf[:n]
					}
				}
				b.isFetching = false
				b.mu.Unlock()
				vtui.FrameManager.Redraw()
			})
		}()
	}
	return nil, piecetable.ErrLoading
}

// LineStartFromEnd reports where the last n lines of the file begin, asking
// the file system to do the counting. It answers false whenever that is not
// possible — a local file, a file system without the feature, a remote host
// without the tool for it, or any error at all — and the caller then falls
// back to reading, which is what it did before this existed.
//
// Two round trips at worst: one for the total, one for the offset. The total
// is kept, so paging around a file that is not growing costs one.
func (b *ViewerBackend) LineStartFromEnd(ctx context.Context, n int64) (int64, bool) {
	if b.indexer == nil || n <= 0 {
		return 0, false
	}
	size := b.Size()

	b.mu.Lock()
	total := b.totalLines
	known := total >= 0 && b.totalForSize == size
	b.mu.Unlock()

	if !known {
		idx, err := b.indexer.LineIndex(ctx, b.path, 1, 0)
		if err != nil || idx.Total < 0 {
			return 0, false
		}
		total = idx.Total
		b.mu.Lock()
		b.totalLines = total
		b.totalForSize = size
		b.mu.Unlock()
	}
	if total <= 0 {
		return 0, false
	}

	first := total - n + 1
	if first < 1 {
		first = 1
	}
	idx, err := b.indexer.LineIndex(ctx, b.path, first, 1)
	if err != nil || len(idx.Offsets) == 0 {
		return 0, false
	}
	return idx.Offsets[0], true
}
func (b *ViewerBackend) FindLineStart(offset int64) int64 {
	if offset <= 0 {
		return 0
	}
	chunkSize := int64(4096)
	curr := offset
	for curr > 0 {
		start := curr - chunkSize
		if start < 0 {
			start = 0
		}

		data, err := b.ReadAt(start, int(curr-start))
		if err == piecetable.ErrLoading {
			return offset // Stay at current offset while loading
		}
		if err != nil {
			return offset
		}

		for i := len(data) - 1; i >= 0; i-- {
			if data[i] == '\n' {
				return start + int64(i) + 1
			}
		}
		curr = start
	}
	return 0
}
