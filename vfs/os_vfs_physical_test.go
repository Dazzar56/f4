package vfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestOSVFS_SupportsPhysicalSize pins the capability contract the
// scanner uses to decide whether to run the lazy-Stat fallback. If
// OSVFS ever returns false here, Windows quick-view would stop
// filling PhysicalBytes even during the actual scan; if a non-OSVFS
// backend returns true, copy/move pre-scan would resume the N+1
// Stat storm the capability gate was introduced to kill.
func TestOSVFS_SupportsPhysicalSize(t *testing.T) {
	var v interface{} = NewOSVFS(".")
	ps, ok := v.(PhysicalSizer)
	if !ok {
		t.Fatal("OSVFS must implement PhysicalSizer")
	}
	if !ps.SupportsPhysicalSize() {
		t.Error("OSVFS.SupportsPhysicalSize should be true")
	}
}

// TestFillPhysicalSize_NilInfo locks in the guarantee that a nil
// FileInfo — which DirEntry.Info() returns when the entry vanished
// between readdir and lstat — doesn't crash the ReadDir path. The
// Unix implementation panicked on nil.Info() before the guard.
func TestFillPhysicalSize_NilInfo(t *testing.T) {
	var item VFSItem
	// Must not panic. PhysicalSize stays at zero.
	fillPhysicalSize(&item, nil, "/nonexistent/path")
	if item.PhysicalSize != 0 {
		t.Errorf("PhysicalSize should stay 0 on nil info, got %d", item.PhysicalSize)
	}
}

// TestFillPhysicalSize_RealFile checks the happy path on the current
// platform. On Unix we get stat.Blocks * 512; on Windows we get
// GetCompressedFileSize; on the "other" stub the value stays 0. In
// the populated cases the number must be:
//   - >= the logical size (dense files never occupy fewer bytes than
//     they contain);
//   - a multiple of 512 (stat.Blocks is in 512-byte units per POSIX;
//     GetCompressedFileSize returns cluster-aligned allocation for
//     regular files, and clusters are always sector-multiples).
func TestFillPhysicalSize_RealFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "hello.bin")
	if err := os.WriteFile(path, make([]byte, 8192), 0644); err != nil {
		t.Fatal(err)
	}
	v := NewOSVFS(tmp)
	item, err := v.Stat(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if item.Size != 8192 {
		t.Errorf("Size = %d, want 8192", item.Size)
	}
	if item.PhysicalSize < 0 {
		t.Errorf("PhysicalSize = %d, must be >= 0", item.PhysicalSize)
	}
	// PhysicalSize == 0 is only expected on the "other" stub; on
	// platforms that populate it the value must sit on a 512-byte
	// boundary and cover the logical size.
	if item.PhysicalSize > 0 {
		if item.PhysicalSize%512 != 0 {
			t.Errorf("PhysicalSize = %d, must be a multiple of 512", item.PhysicalSize)
		}
		if item.PhysicalSize < item.Size {
			t.Errorf("PhysicalSize = %d < Size = %d for a dense file", item.PhysicalSize, item.Size)
		}
	}
}
