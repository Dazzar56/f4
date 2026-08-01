//go:build unix

package vfs

import (
	"os"
	"syscall"
)

// fillPhysicalSize populates item.PhysicalSize with the item's actual
// on-disk footprint. On Unix this is stat.Blocks * 512 (block count is
// always reported in 512-byte units per POSIX, regardless of the fs's
// own block size). This handles sparse files (fewer blocks than Size)
// and filesystems with transparent compression (btrfs/zfs — the block
// count reflects the compressed footprint).
func fillPhysicalSize(item *VFSItem, info os.FileInfo, _ string) {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		item.PhysicalSize = int64(stat.Blocks) * 512
	}
}
