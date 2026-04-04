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
	// Проверяем, выглядит ли файл как архив
	// Если родитель - OSVFS, мы можем проверить файл напрямую
	if osvfs, ok := parent.(*OSVFS); ok {
		absPath, _ := osvfs.Abs(path)
		f, err := os.Open(absPath)
		if err != nil { return false }
		defer f.Close()
		format, _, err := archives.Identify(ctx, filepath.Base(path), f)
		if err != nil || format == nil { return false }
		_, isExtractor := format.(archives.Extractor)
		return isExtractor
	}

	// Если родитель - другая VFS (например, другой архив),
	// проверяем пока только по расширению для скорости (как Far)
	return archives.PathIsArchive(path)
}

func (p *ArchiveProvider) Open(ctx context.Context, parent VFS, path string) (VFS, error) {
	return NewArchiveVFS(parent, path)
}