package archive

import (
	"context"
	"os"
	"path/filepath"

	"github.com/mholt/archives"
	"github.com/unxed/f4/vfs"
)

type ArchiveProvider struct{}

func (p *ArchiveProvider) Name() string { return "mholt/archives" }
func (p *ArchiveProvider) Priority() int { return 10 }

func (p *ArchiveProvider) CanOpen(ctx context.Context, parent vfs.VFS, path string) bool {
	if osvfs, ok := parent.(*vfs.OSVFS); ok {
		absPath, _ := osvfs.Abs(path)
		f, err := os.Open(absPath)
		if err != nil { return false }
		defer f.Close()
		format, _, err := archives.Identify(ctx, filepath.Base(path), f)
		if err != nil || format == nil { return false }
		_, isExtractor := format.(archives.Extractor)
		return isExtractor
	}
	return archives.PathIsArchive(path)
}

func (p *ArchiveProvider) Open(ctx context.Context, parent vfs.VFS, path string) (vfs.VFS, error) {
	return NewArchiveVFS(parent, path)
}