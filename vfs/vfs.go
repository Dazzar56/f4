package vfs

import (
	"context"
	"io"
	"time"
)

// VFSItem represents a generic file or directory entry.
type VFSItem struct {
	Name         string
	Size         int64
	IsDir        bool
	MTime        time.Time
	Mode         string
	IsExecutable bool
}

// VFSCapabilities defines what the current VFS implementation can do efficiently.
type VFSCapabilities struct {
	HasServerSideCopy bool
	HasServerSideMove bool
	HasRandomAccess   bool // Supports ReadAt
	HasSearch         bool // Supports server-side search
}

// VFS is the core interface for file operations in f4.
type VFS interface {
	GetPath() string
	SetPath(path string) error
	ReadDir(ctx context.Context, path string, onChunk func([]VFSItem)) error
	Stat(ctx context.Context, path string) (VFSItem, error)
	Join(elem ...string) string
	Abs(path string) (string, error)
	Base(path string) string
	Dir(path string) string

	// Mutations
	MkDir(ctx context.Context, path string) error
	Remove(ctx context.Context, path string) error
	Rename(ctx context.Context, oldpath, newpath string) error

	// Advanced / Remote Operations
	GetCapabilities() VFSCapabilities

	// Random Access (required for high-performance Viewer/Editor)
	// Open returns a ReadAtCloser for the file.
	Open(ctx context.Context, path string) (ReadAtCloser, error)

	// Create returns a WriteCloser for new files.
	Create(ctx context.Context, path string) (io.WriteCloser, error)
}

// ReadAtCloser combines reader interfaces with context support.
type ReadAtCloser interface {
	ReadAt(ctx context.Context, p []byte, off int64) (n int, err error)
	Read(ctx context.Context, p []byte) (n int, err error)
	io.Closer
	Size() int64
}