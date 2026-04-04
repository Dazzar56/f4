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
	Search(ctx context.Context, path string, pattern string) (chan int64, error)

	// Random Access (required for high-performance Viewer/Editor)
	// Open returns a ReadAtCloser for the file.
	Open(ctx context.Context, path string) (ReadAtCloser, error)

	// Create returns a WriteCloser for new files.
	Create(ctx context.Context, path string) (io.WriteCloser, error)
	ParentVFS() VFS // Returns the underlying VFS if this is a virtual mount, or nil
}

// VFSProvider умеет определять, может ли он открыть путь, и создавать экземпляр VFS.
type VFSProvider interface {
	Name() string
	// Priority: чем выше, тем раньше провайдер опрашивается (архивы обычно имеют низкий приоритет)
	Priority() int
	// CanOpen возвращает true, если провайдер понимает этот путь.
	// parent — текущая VFS, в которой находится объект.
	CanOpen(ctx context.Context, parent VFS, path string) bool
	// Open создает новый экземпляр VFS.
	Open(ctx context.Context, parent VFS, path string) (VFS, error)
}

var providers []VFSProvider

func RegisterProvider(p VFSProvider) {
	providers = append(providers, p)
	// Сортируем по приоритету
}

func FindProvider(ctx context.Context, parent VFS, path string) VFSProvider {
	for _, p := range providers {
		if p.CanOpen(ctx, parent, path) {
			return p
		}
	}
	return nil
}

// ReadAtCloser combines reader interfaces with context support.
type ReadAtCloser interface {
	ReadAt(ctx context.Context, p []byte, off int64) (n int, err error)
	Read(ctx context.Context, p []byte) (n int, err error)
	io.Closer
	Size() int64
}