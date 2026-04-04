package vfs

import (
	"context"
	"os"
	"path/filepath"

	"github.com/mholt/archives"
)

type ArchiveProvider struct{}

func (p *ArchiveProvider) Name() string { return "mholt/archives" }
func (p *ArchiveProvider) Priority() int { return 10 } // Низкий приоритет

func (p *ArchiveProvider) CanOpen(ctx context.Context, parent VFS, path string) bool {
	// Провайдер архивов работает только с файлами в OSVFS (пока что)
	if _, ok := parent.(*OSVFS); !ok {
		return false
	}

	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	format, _, err := archives.Identify(ctx, filepath.Base(path), f)
	if err != nil || format == nil {
		return false
	}

	_, isExtractor := format.(archives.Extractor)
	return isExtractor
}

func (p *ArchiveProvider) Open(ctx context.Context, parent VFS, path string) (VFS, error) {
	return NewArchiveVFS(parent, path)
}