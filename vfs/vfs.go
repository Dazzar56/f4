package vfs

import (
	"io"
	"time"
)

// VFSItem represents a generic file or directory entry.
type VFSItem struct {
	Name  string
	Size  int64
	IsDir bool
	MTime time.Time
	Mode  string
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
	ReadDir(path string) ([]VFSItem, error)
	Stat(path string) (VFSItem, error)
	Join(elem ...string) string
	Abs(path string) (string, error)
	Base(path string) string
	Dir(path string) string

	// Mutations
	MkDir(path string) error
	Remove(path string) error
	Rename(oldpath, newpath string) error

	// Advanced / Remote Operations
	GetCapabilities() VFSCapabilities

	// Random Access (required for high-performance Viewer/Editor)
	// Open returns a ReadAtCloser for the file.
	Open(path string) (ReadAtCloser, error)

	// Create returns a WriteCloser for new files.
	Create(path string) (io.WriteCloser, error)
}

// ReadAtCloser combines io.ReaderAt, io.Reader and io.Closer.
type ReadAtCloser interface {
	io.ReaderAt
	io.Reader
	io.Closer
	Size() int64
}