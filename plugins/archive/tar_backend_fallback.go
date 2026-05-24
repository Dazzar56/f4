//go:build dragonfly || netbsd

package archive

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mholt/archives"
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

	var format archives.Archiver
	tarFmt := archives.Tar{}

	if strings.HasSuffix(lowerName, ".gz") || strings.HasSuffix(lowerName, ".tgz") {
		format = archives.CompressedArchive{Compression: archives.Gz{}, Archival: tarFmt}
	} else if strings.HasSuffix(lowerName, ".bz2") {
		format = archives.CompressedArchive{Compression: archives.Bz2{}, Archival: tarFmt}
	} else if strings.HasSuffix(lowerName, ".xz") || strings.HasSuffix(lowerName, ".txz") {
		format = archives.CompressedArchive{Compression: archives.Xz{}, Archival: tarFmt}
	} else if strings.HasSuffix(lowerName, ".zst") {
		format = archives.CompressedArchive{Compression: archives.Zstd{}, Archival: tarFmt}
	} else {
		format = tarFmt
	}

	var files []archives.FileInfo
	for p, fi := range fileMap {
		rel, err := filepath.Rel(basePath, p)
		if err != nil {
			continue
		}
		nameInArchive := filepath.ToSlash(rel)

		capturePath := p
		files = append(files, archives.FileInfo{
			FileInfo:      fi,
			NameInArchive: nameInArchive,
			Open: func() (fs.File, error) {
				return os.Open(capturePath)
			},
		})
	}

	return format.Archive(ctx, out, files)
}