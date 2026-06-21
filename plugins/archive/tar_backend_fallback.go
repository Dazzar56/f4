//go:build dragonfly || netbsd || solaris || illumos

package archive

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/unxed/zipper/archive"
)

func tarOpenFS(path string) (any, error) {
	return nil, fmt.Errorf("fast tar not supported on dragonfly")
}

func tarRemoveIndex(path string) {}

func tarExtract(ctx context.Context, srcPath, destDir string) error {
	return fmt.Errorf("fast tar not supported on dragonfly") // вызовет fallback на mholt/archives в вызывающем коде
}

func tarArchive(ctx context.Context, arcPath, basePath string, fileMap map[string]os.FileInfo, lowerName string) error {
	out, err := os.Create(arcPath)
	if err != nil {
		return err
	}
	defer out.Close()

	var format archive.Archiver
	tarFmt := archive.Tar{}

	if strings.HasSuffix(lowerName, ".gz") || strings.HasSuffix(lowerName, ".tgz") {
		format = archive.CompressedArchive{Compression: archive.Gz{}, Archival: tarFmt}
	} else if strings.HasSuffix(lowerName, ".bz2") {
		format = archive.CompressedArchive{Compression: archive.Bz2{}, Archival: tarFmt}
	} else if strings.HasSuffix(lowerName, ".xz") || strings.HasSuffix(lowerName, ".txz") {
		format = archive.CompressedArchive{Compression: archive.Xz{}, Archival: tarFmt}
	} else if strings.HasSuffix(lowerName, ".zst") {
		format = archive.CompressedArchive{Compression: archive.Zstd{}, Archival: tarFmt}
	} else {
		format = tarFmt
	}

	var files []archive.FileInfo
	for p, fi := range fileMap {
		rel, err := filepath.Rel(basePath, p)
		if err != nil {
			continue
		}
		nameInArchive := filepath.ToSlash(rel)

		capturePath := p
		files = append(files, archive.FileInfo{
			FileInfo:      fi,
			NameInArchive: nameInArchive,
			Open: func() (fs.File, error) {
				return os.Open(capturePath)
			},
		})
	}

	return format.Archive(ctx, out, files)
}
