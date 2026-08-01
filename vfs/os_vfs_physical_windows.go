//go:build windows

package vfs

import (
	"os"
	"syscall"
	"unsafe"
)

// GetCompressedFileSizeW isn't exposed by x/sys/windows in a way we
// can use ergonomically here, so we bind it via NewLazyDLL. Returns
// the low-order DWORD of the size and writes the high-order DWORD to
// *lpFileSizeHigh. INVALID_FILE_SIZE (0xFFFFFFFF) signals failure —
// GetLastError() is needed to disambiguate from an actual file that
// happens to be 4 GiB−1 bytes, but we treat any 0xFFFFFFFF low DWORD
// as a soft failure (the fallback below is harmless).
var (
	physKernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetCompressedFileSizeW = physKernel32.NewProc("GetCompressedFileSizeW")
)

// fillPhysicalSizeCheap is a no-op on Windows — there's no way to get
// on-disk allocation size from FileInfo alone; GetCompressedFileSize
// is a separate syscall we don't want to pay in the ReadDir path (an
// extra kernel round-trip per file, and on SMB an extra network
// round-trip too). Consumers that actually need PhysicalSize (the
// QuickView scan) go through fillPhysicalSize / Stat instead.
func fillPhysicalSizeCheap(_ *VFSItem, _ os.FileInfo) {}

// fillPhysicalSize asks NTFS for the on-disk footprint of path, which
// matches far/far2 semantics: NTFS-compressed files return their
// compressed size, sparse regions are excluded, and plain files return
// their cluster-aligned allocation. Non-NTFS or unsupported paths
// (some network shares, some FUSE mounts) fall back to info.Size().
func fillPhysicalSize(item *VFSItem, info os.FileInfo, path string) {
	if path == "" || info == nil {
		return
	}
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		item.PhysicalSize = info.Size()
		return
	}
	var high uint32
	r, _, _ := procGetCompressedFileSizeW.Call(
		uintptr(unsafe.Pointer(ptr)),
		uintptr(unsafe.Pointer(&high)),
	)
	low := uint32(r)
	if low == 0xFFFFFFFF {
		item.PhysicalSize = info.Size()
		return
	}
	item.PhysicalSize = int64((uint64(high) << 32) | uint64(low))
}

// SupportsPhysicalSize is true on Windows — see the Unix version
// for the rationale of keeping the answer per-platform.
func (v *OSVFS) SupportsPhysicalSize() bool { return true }

// isReparsePoint reports whether the entry described by info is any
// kind of NTFS reparse point — symlinks, junctions (mount points),
// OneDrive/Dropbox placeholders, etc. Go's Mode()&ModeSymlink covers
// the plain symlink case, but its handling of junctions has flipped
// between releases (ModeSymlink vs ModeIrregular); relying on it
// alone was letting the scanner walk INTO junctions like
// C:\Users\<user>\AppData\Local\Application Data which points back
// at its own parent — cue millions of ghost files and hundreds of
// gigabytes of "physical" size that the disk doesn't have.
// FILE_ATTRIBUTE_REPARSE_POINT is the authoritative NTFS bit.
func isReparsePoint(info os.FileInfo) bool {
	if info == nil {
		return false
	}
	if a, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return a.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
	}
	return false
}
