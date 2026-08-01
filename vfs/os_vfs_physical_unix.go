//go:build unix

package vfs

import (
	"os"
	"syscall"
)

// fillPhysicalSizeCheap populates PhysicalSize only when the answer
// is already in memory alongside FileInfo — on Unix, stat.Blocks is
// part of Stat_t, which lstat() had to load anyway. So calling this
// from the ReadDir loop is free. Windows and stub variants no-op.
//
// info can legitimately be nil — DirEntry.Info() returns nil,err when
// the entry vanished between readdir and lstat, common in /tmp/ or
// build trees. We simply leave PhysicalSize at zero rather than panic.
func fillPhysicalSizeCheap(item *VFSItem, info os.FileInfo) {
	if info == nil {
		return
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		// stat.Blocks is always in 512-byte units per POSIX, regardless
		// of the fs's own block size. Handles sparse files (fewer blocks
		// than Size) and transparent-compression fs (btrfs/zfs — Blocks
		// reflects the compressed footprint).
		item.PhysicalSize = int64(stat.Blocks) * 512
	}
}

// fillPhysicalSize does whatever it takes to populate PhysicalSize.
// On Unix this is the same as fillPhysicalSizeCheap. On Windows the
// two diverge — see os_vfs_physical_windows.go.
func fillPhysicalSize(item *VFSItem, info os.FileInfo, _ string) {
	fillPhysicalSizeCheap(item, info)
}
