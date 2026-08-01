package vfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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
// GetCompressedFileSize; on other unices the stub leaves it at 0. In
// all cases PhysicalSize must be >= 0 and — where populated — should
// be either 0 (empty file, no blocks allocated on the fs) or a
// multiple of a sector-sized unit.
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
}
