//go:build !dragonfly && !netbsd && !solaris && !illumos

package archive

import (
	"context"
	"os"
	"strings"

	"github.com/unxed/tar"
)

func tarOpenFS(path string) (any, error) {
	return tar.NewFS(path, "")
}

func tarRemoveIndex(path string) {
	idx, _ := tar.GetStandardIndexPath(path)
	os.Remove(idx)
}

func tarExtract(ctx context.Context, srcPath, destDir string) error {
	e, err := tar.NewExtractor(srcPath, destDir)
	if err != nil {
		return err
	}
	defer e.Close()
	return e.Extract(ctx)
}

func tarArchive(ctx context.Context, arcPath, basePath string, fileMap map[string]os.FileInfo, lowerName string) error {
	method := tar.Store
	if strings.HasSuffix(lowerName, ".gz") || strings.HasSuffix(lowerName, ".tgz") {
		method = tar.GZIP
	} else if strings.HasSuffix(lowerName, ".bz2") {
		method = tar.BZIP2
	} else if strings.HasSuffix(lowerName, ".xz") || strings.HasSuffix(lowerName, ".txz") {
		method = tar.XZ
	} else if strings.HasSuffix(lowerName, ".zst") {
		method = tar.ZSTD
	}

	idxPath, _ := tar.GetStandardIndexPath(arcPath)
	archiver, err := tar.NewArchiver(arcPath, basePath, tar.WithArchiverMethod(method), tar.WithArchiverIndex(idxPath))
	if err != nil {
		return err
	}
	defer archiver.Close()
	return archiver.Archive(ctx, fileMap)
}
