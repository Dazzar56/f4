package vfs

import (
	"context"
	"io"
	"os"
	"path"
	"time"
)

// NullVFS is a mock filesystem for testing UI responsiveness and file operations.
// It provides virtual files of specific sizes and discards any written data,
// simulating network or disk delays via a speed limit.
type NullVFS struct {
	currentPath string
	speedLimit  int64 // Bytes per second. 0 means unlimited.
}

var nullFiles = map[string]int64{
	"1KB.bin":   1024,
	"1MB.bin":   1024 * 1024,
	"10MB.bin":  10 * 1024 * 1024,
	"100MB.bin": 100 * 1024 * 1024,
	"1GB.bin":   1024 * 1024 * 1024,
}

// NewNullVFS creates a new NullVFS with the specified speed limit in bytes per second.
func NewNullVFS(speedLimit int64) *NullVFS {
	return &NullVFS{
		currentPath: "/",
		speedLimit:  speedLimit,
	}
}

func (v *NullVFS) GetPath() string { return v.currentPath }

func (v *NullVFS) IsAtRoot() bool {
	return v.currentPath == "/"
}

func (v *NullVFS) SetPath(p string) error {
	v.currentPath = path.Clean(p)
	return nil
}

func (v *NullVFS) ReadDir(ctx context.Context, p string, onChunk func([]VFSItem)) error {
	p = path.Clean(p)
	var items []VFSItem

	// Only show dummy files at the root
	if p == "/" {
		for name, size := range nullFiles {
			items = append(items, VFSItem{
				Name:  name,
				Size:  size,
				IsDir: false,
				MTime: time.Now(),
			})
		}
		items = append(items, VFSItem{
			Name:  "upload",
			Size:  0,
			IsDir: true,
			MTime: time.Now(),
		})
	}

	if len(items) > 0 && onChunk != nil {
		onChunk(items)
	}
	return nil
}

func (v *NullVFS) Stat(ctx context.Context, p string) (VFSItem, error) {
	p = path.Clean(p)
	base := path.Base(p)

	if p == "/" || base == "upload" {
		return VFSItem{Name: base, IsDir: true, MTime: time.Now()}, nil
	}

	if size, ok := nullFiles[base]; ok && path.Dir(p) == "/" {
		return VFSItem{Name: base, Size: size, IsDir: false, MTime: time.Now()}, nil
	}

	// For uploaded files, just pretend they exist with size 0
	return VFSItem{Name: base, Size: 0, IsDir: false, MTime: time.Now()}, nil
}

func (v *NullVFS) Join(elem ...string) string { return path.Join(elem...) }

func (v *NullVFS) Abs(p string) (string, error) {
	if path.IsAbs(p) {
		return path.Clean(p), nil
	}
	return path.Join(v.currentPath, p), nil
}

func (v *NullVFS) Base(p string) string { return path.Base(p) }
func (v *NullVFS) Dir(p string) string  { return path.Dir(p) }

// Mutations succeed silently
func (v *NullVFS) MkDir(ctx context.Context, p string) error         { return nil }
func (v *NullVFS) Remove(ctx context.Context, p string) error        { return nil }
func (v *NullVFS) Rename(ctx context.Context, old, new string) error { return nil }

func (v *NullVFS) GetCapabilities() VFSCapabilities {
	return VFSCapabilities{
		HasServerSideCopy: false,
		HasServerSideMove: false,
		HasRandomAccess:   true,
		HasSearch:         false,
	}
}

func (v *NullVFS) Search(ctx context.Context, p string, pattern string) (chan int64, error) {
	return nil, nil
}

func (v *NullVFS) Open(ctx context.Context, p string) (ReadAtCloser, error) {
	base := path.Base(p)
	size, ok := nullFiles[base]
	if !ok {
		size = 0 // "Uploaded" files
	}
	return &nullReader{size: size, speed: v.speedLimit}, nil
}

func (v *NullVFS) Create(ctx context.Context, p string) (io.WriteCloser, error) {
	// Refuse overwriting static files, allow everything else
	base := path.Base(p)
	if _, ok := nullFiles[base]; ok && path.Dir(p) == "/" {
		return nil, os.ErrPermission
	}
	return &nullWriter{ctx: ctx, speed: v.speedLimit}, nil
}

func (v *NullVFS) ParentVFS() VFS { return nil }
func (v *NullVFS) Clone() VFS     { return NewNullVFS(v.speedLimit) }
func (v *NullVFS) Close() error   { return nil }

// --- Throttled Reader ---

type nullReader struct {
	size   int64
	offset int64
	speed  int64
}

func (r *nullReader) Size() int64 { return r.size }

func (r *nullReader) Read(ctx context.Context, p []byte) (n int, err error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if r.offset >= r.size {
		return 0, io.EOF
	}
	n = len(p)
	if r.offset+int64(n) > r.size {
		n = int(r.size - r.offset)
	}

	// Zero-fill
	for i := 0; i < n; i++ {
		p[i] = 0
	}

	r.offset += int64(n)
	err = throttle(ctx, n, r.speed)
	return n, err
}

func (r *nullReader) ReadAt(ctx context.Context, p []byte, off int64) (n int, err error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if off >= r.size {
		return 0, io.EOF
	}
	n = len(p)
	if off+int64(n) > r.size {
		n = int(r.size - off)
	}

	for i := 0; i < n; i++ {
		p[i] = 0
	}

	err = throttle(ctx, n, r.speed)
	return n, err
}

func (r *nullReader) Close() error { return nil }

// --- Throttled Writer ---

type nullWriter struct {
	ctx   context.Context
	speed int64
}

func (w *nullWriter) Write(p []byte) (n int, err error) {
	n = len(p)
	err = throttle(w.ctx, n, w.speed)
	return n, err
}

func (w *nullWriter) Close() error { return nil }

// --- Throttle Helper ---

func throttle(ctx context.Context, n int, speed int64) error {
	if speed <= 0 || n <= 0 {
		return nil
	}
	dur := time.Duration(float64(n) / float64(speed) * float64(time.Second))

	timer := time.NewTimer(dur)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}