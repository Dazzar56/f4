package vfs

import (
	"context"
	"io"
	"os"
	"time"
	"path/filepath"
)

type OSVFS struct {
	currentPath string
}

func NewOSVFS(initialPath string) *OSVFS {
	abs, _ := filepath.Abs(initialPath)
	return &OSVFS{currentPath: abs}
}

func (v *OSVFS) GetPath() string { return v.currentPath }
func (v *OSVFS) SetPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil { return err }
	v.currentPath = abs
	return nil
}

func (v *OSVFS) ReadDir(ctx context.Context, path string, onChunk func([]VFSItem)) error {
	f, err := os.Open(path)
	if err != nil { return err }
	defer f.Close()

	for {
		if ctx.Err() != nil { return ctx.Err() }
		entries, err := f.ReadDir(1000)
		if err != nil {
			if err == io.EOF { break }
			return err
		}

		items := make([]VFSItem, 0, len(entries))
		for _, e := range entries {
			info, _ := e.Info()
			var size int64
			var mtime time.Time
			var isExec bool
			if info != nil {
				size = info.Size()
				mtime = info.ModTime()
				isExec = info.Mode().Perm()&0111 != 0
			}
			items = append(items, VFSItem{
				Name:         e.Name(),
				Size:         size,
				IsDir:        e.IsDir(),
				MTime:        mtime,
				IsExecutable: isExec,
			})
		}

		if len(items) > 0 && onChunk != nil {
			onChunk(items)
		}
	}
	return nil
}

func (v *OSVFS) Stat(ctx context.Context, path string) (VFSItem, error) {
	if ctx.Err() != nil { return VFSItem{}, ctx.Err() }
	info, err := os.Stat(path)
	if err != nil { return VFSItem{}, err }
	return VFSItem{
		Name:         info.Name(),
		Size:         info.Size(),
		IsDir:        info.IsDir(),
		MTime:        info.ModTime(),
		IsExecutable: info.Mode().Perm()&0111 != 0,
	}, nil
}

func (v *OSVFS) Join(elem ...string) string      { return filepath.Join(elem...) }
func (v *OSVFS) Abs(path string) (string, error) { return filepath.Abs(path) }
func (v *OSVFS) Base(path string) string         { return filepath.Base(path) }
func (v *OSVFS) Dir(path string) string          { return filepath.Dir(path) }
func (v *OSVFS) MkDir(ctx context.Context, path string) error         { if ctx.Err() != nil { return ctx.Err() }; return os.MkdirAll(path, 0755) }
func (v *OSVFS) Remove(ctx context.Context, path string) error        { if ctx.Err() != nil { return ctx.Err() }; return os.RemoveAll(path) }
func (v *OSVFS) Rename(ctx context.Context, old, new string) error    { if ctx.Err() != nil { return ctx.Err() }; return os.Rename(old, new) }

func (v *OSVFS) GetCapabilities() VFSCapabilities {
	return VFSCapabilities{
		HasServerSideCopy: true,
		HasServerSideMove: true,
		HasRandomAccess:   true,
		HasSearch:         false, // TODO: Implement via local Find
	}
}

type osFileWrapper struct {
	*os.File
	size int64
}

func (f *osFileWrapper) Size() int64 { return f.size }
func (f *osFileWrapper) Read(p []byte) (n int, err error) { return f.File.Read(p) }

func (v *OSVFS) Open(ctx context.Context, path string) (ReadAtCloser, error) {
	if ctx.Err() != nil { return nil, ctx.Err() }
	f, err := os.Open(path)
	if err != nil { return nil, err }
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &osFileWrapper{File: f, size: info.Size()}, nil
}

func (v *OSVFS) Create(ctx context.Context, path string) (io.WriteCloser, error) {
	if ctx.Err() != nil { return nil, ctx.Err() }
	return os.Create(path)
}