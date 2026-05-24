//go:build !dragonfly

package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/tar"
	"github.com/unxed/vtui"
)

func isTarArchive(path string) bool {
	name := strings.ToLower(path)
	return strings.HasSuffix(name, ".tar") ||
		strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tgz") ||
		strings.HasSuffix(name, ".tar.bz2") || strings.HasSuffix(name, ".tbz2") ||
		strings.HasSuffix(name, ".tar.xz") || strings.HasSuffix(name, ".txz") ||
		strings.HasSuffix(name, ".tar.zst") || strings.HasSuffix(name, ".tar.zstd")
}

func handleArchiveIndexOp(srcVfs vfs.VFS, oldPath string, dstVfs vfs.VFS, newPath string, isMove bool) {
	if !isTarArchive(oldPath) {
		return
	}

	absOld, _ := srcVfs.Abs(oldPath)
	absNew, _ := dstVfs.Abs(newPath)

	oldIdx, _ := tar.GetStandardIndexPath(absOld)
	newIdx, _ := tar.GetStandardIndexPath(absNew)

	if _, err := os.Stat(oldIdx); err == nil {
		if isMove {
			vtui.DebugLog("FILEOP: Moving archive index: %s -> %s", oldIdx, newIdx)
			os.Rename(oldIdx, newIdx)
		} else {
			vtui.DebugLog("FILEOP: Copying archive index: %s -> %s", oldIdx, newIdx)
			s, err := os.Open(oldIdx)
			if err != nil {
				return
			}
			defer s.Close()

			os.MkdirAll(filepath.Dir(newIdx), 0755)
			d, err := os.Create(newIdx)
			if err != nil {
				return
			}
			defer d.Close()

			io.Copy(d, s)
		}
	}
}

func handleArchiveIndexDelete(ctx context.Context, v vfs.VFS, p string) {
	st, err := v.Stat(ctx, p)
	if err != nil {
		return
	}

	if st.IsDir {
		v.ReadDir(ctx, p, func(items []vfs.VFSItem) {
			for _, itm := range items {
				if itm.Name == ".." {
					continue
				}
				handleArchiveIndexDelete(ctx, v, v.Join(p, itm.Name))
			}
		})
	} else if isTarArchive(p) {
		abs, _ := v.Abs(p)
		idx, _ := tar.GetStandardIndexPath(abs)
		if _, err := os.Stat(idx); err == nil {
			vtui.DebugLog("FILEOP: Deleting archive index: %s", idx)
			os.Remove(idx)
		}
	}
}