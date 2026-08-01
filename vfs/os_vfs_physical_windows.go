//go:build windows

package vfs

import (
	"context"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
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

// FileIdentity resolves a path's NTFS file identity via
// GetFileInformationByHandle: the volume serial number plus the 64-bit
// file index uniquely identify a file record, so every hard link to
// one file reports the same (device, inode). This is what lets the
// scanner's DedupInodes pass count hard-linked files once on Windows,
// matching the Unix Stat_t path — Go's ReadDir (FindNextFile) can't
// supply a file index, so we open the entry here. FILE_FLAG_BACKUP_
// SEMANTICS lets us open directories too; FILE_FLAG_OPEN_REPARSE_POINT
// makes us identify a reparse point itself rather than its target. Any
// failure (vanished path, access denied, or a volume like FAT/some SMB
// shares that returns a zero index) yields ok=false and the scanner
// simply skips dedup for that entry.
func (v *OSVFS) FileIdentity(ctx context.Context, path string) (device, inode uint64, ok bool) {
	if ctx.Err() != nil {
		return 0, 0, false
	}
	ptr, err := windows.UTF16PtrFromString(prepareOSPath(path))
	if err != nil {
		return 0, 0, false
	}
	h, err := windows.CreateFile(
		ptr,
		0, // querying metadata needs no access rights
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return 0, 0, false
	}
	defer windows.CloseHandle(h)

	var bhfi windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &bhfi); err != nil {
		return 0, 0, false
	}
	inode = (uint64(bhfi.FileIndexHigh) << 32) | uint64(bhfi.FileIndexLow)
	// A zero index means the volume doesn't expose persistent file IDs
	// (FAT, some network shares). Treat it as "no identity" so we never
	// merge unrelated entries under (serial, 0).
	if inode == 0 {
		return 0, 0, false
	}
	return uint64(bhfi.VolumeSerialNumber), inode, true
}

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
