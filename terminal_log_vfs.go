package main

import (
	"context"
	"io"
	"os"

	"github.com/unxed/f4/vfs"
)

type TerminalLogVFS struct {
	tv *TerminalView
}

func (v *TerminalLogVFS) IsAtRoot() bool { return true }
func (v *TerminalLogVFS) GetPath() string { return "term://" }
func (v *TerminalLogVFS) SetPath(path string) error { return nil }
func (v *TerminalLogVFS) ReadDir(ctx context.Context, path string, onChunk func([]vfs.VFSItem)) error { return nil }
func (v *TerminalLogVFS) Stat(ctx context.Context, path string) (vfs.VFSItem, error) {
	return vfs.VFSItem{Name: "Terminal Log", IsDir: false}, nil
}
func (v *TerminalLogVFS) Join(elem ...string) string {
	if len(elem) == 0 { return "" }
	return elem[len(elem)-1]
}
func (v *TerminalLogVFS) Abs(path string) (string, error) { return path, nil }
func (v *TerminalLogVFS) Base(path string) string { return path }
func (v *TerminalLogVFS) Dir(path string) string { return "term://" }
func (v *TerminalLogVFS) MkDir(ctx context.Context, path string) error { return os.ErrPermission }
func (v *TerminalLogVFS) Remove(ctx context.Context, path string) error { return os.ErrPermission }
func (v *TerminalLogVFS) Rename(ctx context.Context, oldpath, newpath string) error { return os.ErrPermission }
func (v *TerminalLogVFS) SetAttributes(ctx context.Context, path string, item vfs.VFSItem) error { return os.ErrPermission }
func (v *TerminalLogVFS) GetCapabilities() vfs.VFSCapabilities { return vfs.VFSCapabilities{HasRandomAccess: true} }
func (v *TerminalLogVFS) Search(ctx context.Context, path string, pattern string) (chan int64, error) { return nil, nil }
func (v *TerminalLogVFS) Create(ctx context.Context, path string) (io.WriteCloser, error) { return nil, os.ErrPermission }
func (v *TerminalLogVFS) ParentVFS() vfs.VFS { return nil }
func (v *TerminalLogVFS) Clone() vfs.VFS { return v }
func (v *TerminalLogVFS) Close() error { return nil }

func (v *TerminalLogVFS) Open(ctx context.Context, path string) (vfs.ReadAtCloser, error) {
	return &terminalLogWrapper{tv: v.tv}, nil
}

type terminalLogWrapper struct {
	tv *TerminalView
}

func (w *terminalLogWrapper) Size() int64 {
	w.tv.mu.Lock()
	defer w.tv.mu.Unlock()
	return int64(w.tv.pt.Size() + len(w.tv.pendingLog))
}

func (w *terminalLogWrapper) ReadAt(ctx context.Context, p []byte, off int64) (int, error) {
	w.tv.mu.Lock()
	defer w.tv.mu.Unlock()

	w.tv.flushLogUnsafe()

	size := w.tv.pt.Size()
	if off >= int64(size) {
		return 0, io.EOF
	}

	readLen := len(p)
	if off+int64(readLen) > int64(size) {
		readLen = int(int64(size) - off)
	}

	data, err := w.tv.pt.GetRange(int(off), readLen)
	if len(data) > 0 {
		copy(p, data)
	}

	if err != nil {
		return len(data), err
	}
	if len(data) < len(p) {
		return len(data), io.EOF
	}
	return len(data), nil
}

func (w *terminalLogWrapper) Read(ctx context.Context, p []byte) (int, error) {
	return 0, io.EOF
}

func (w *terminalLogWrapper) Close() error { return nil }