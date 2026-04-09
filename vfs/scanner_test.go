package vfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"strings"
	"fmt"
)

func TestOpStats_Add(t *testing.T) {
	s1 := OpStats{Bytes: 100, Files: 2, Dirs: 1}
	s2 := OpStats{Bytes: 50, Files: 1, Dirs: 0}
	s1.Add(s2)

	if s1.Bytes != 150 || s1.Files != 3 || s1.Dirs != 1 {
		t.Errorf("OpStats.Add failed: %+v", s1)
	}
}

func TestGenericScan_FlatFiles(t *testing.T) {
	tmpDir := t.TempDir()
	v := NewOSVFS(tmpDir)

	os.WriteFile(filepath.Join(tmpDir, "f1.txt"), []byte("abc"), 0644) // 3 bytes
	os.WriteFile(filepath.Join(tmpDir, "f2.txt"), []byte("defg"), 0644) // 4 bytes

	stats, err := GenericScan(context.Background(), v, tmpDir, []string{"f1.txt", "f2.txt"}, nil)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if stats.Bytes != 7 {
		t.Errorf("Expected 7 bytes, got %d", stats.Bytes)
	}
	if stats.Files != 2 {
		t.Errorf("Expected 2 files, got %d", stats.Files)
	}
	if stats.Dirs != 0 {
		t.Errorf("Expected 0 dirs, got %d", stats.Dirs)
	}
}

func TestGenericScan_Recursive(t *testing.T) {
	tmpDir := t.TempDir()
	v := NewOSVFS(tmpDir)

	// Structure:
	// /root_dir (Dir)
	// /root_dir/file1.txt (5 bytes)
	// /root_dir/sub_dir (Dir)
	// /root_dir/sub_dir/file2.txt (10 bytes)

	rootDir := filepath.Join(tmpDir, "root_dir")
	subDir := filepath.Join(rootDir, "sub_dir")
	os.MkdirAll(subDir, 0755)

	os.WriteFile(filepath.Join(rootDir, "file1.txt"), make([]byte, 5), 0644)
	os.WriteFile(filepath.Join(subDir, "file2.txt"), make([]byte, 10), 0644)

	var lastReportedPath string
	callCount := 0
	cb := func(currentPath string, currentStats OpStats) {
		lastReportedPath = currentPath
		callCount++
	}

	stats, err := GenericScan(context.Background(), v, tmpDir, []string{"root_dir"}, cb)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// 2 files, 2 directories (root_dir, sub_dir)
	if stats.Bytes != 15 {
		t.Errorf("Expected 15 bytes, got %d", stats.Bytes)
	}
	if stats.Files != 2 {
		t.Errorf("Expected 2 files, got %d", stats.Files)
	}
	if stats.Dirs != 2 {
		t.Errorf("Expected 2 dirs, got %d", stats.Dirs)
	}

	// Callback should be called for root_dir, sub_dir, and both files
	if callCount != 4 {
		t.Errorf("Expected callback to be called 4 times, got %d", callCount)
	}
	if lastReportedPath == "" {
		t.Error("Callback path was not populated")
	}
}

func TestGenericScan_Cancellation(t *testing.T) {
	// We use NullVFS to ensure predictable deep structure without disk IO
	// But NullVFS currently doesn't simulate deep folders easily,
	// so let's use OSVFS and just create a few folders, then cancel.
	tmpDir := t.TempDir()
	v := NewOSVFS(tmpDir)

	os.MkdirAll(filepath.Join(tmpDir, "dir1", "dir2", "dir3"), 0755)

	ctx, cancel := context.WithCancel(context.Background())

	cb := func(currentPath string, currentStats OpStats) {
		// Cancel immediately after seeing the first item
		cancel()
	}

	_, err := GenericScan(ctx, v, tmpDir, []string{"dir1"}, cb)
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}

func TestCalculateStats_FastScannerSupport(t *testing.T) {
	// A dummy VFS that implements FastScanner
	fastVFS := &mockFastVFS{
		OSVFS: *NewOSVFS("."),
	}

	stats, err := CalculateStats(context.Background(), fastVFS, "/", []string{"dummy"}, nil)
	if err != nil {
		t.Fatalf("FastScan failed: %v", err)
	}

	if stats.Bytes != 999 {
		t.Errorf("Expected FastScanner to be used (Bytes=999), got %d", stats.Bytes)
	}
	if !fastVFS.scanCalled {
		t.Error("FastScanner.Scan was not called")
	}
}
func TestGenericScan_Empty(t *testing.T) {
	v := NewOSVFS(t.TempDir())
	stats, err := GenericScan(context.Background(), v, "/", []string{}, nil)
	if err != nil {
		t.Errorf("Empty scan should not return error, got %v", err)
	}
	if stats.Bytes != 0 || stats.Files != 0 || stats.Dirs != 0 {
		t.Errorf("Empty scan should return zero stats, got %+v", stats)
	}
}

func TestGenericScan_DepthLimit(t *testing.T) {
	mv := &mockScannerVFS{}
	// Setup a self-referencing "infinite" directory structure in the mock
	mv.onStat = func(p string) VFSItem {
		return VFSItem{Name: "dir", IsDir: true}
	}
	mv.onReadDir = func(p string) []VFSItem {
		return []VFSItem{{Name: "subdir", IsDir: true}}
	}

	_, err := GenericScan(context.Background(), mv, "/", []string{"root"}, nil)
	if err == nil || !strings.Contains(err.Error(), "maximum recursion depth exceeded") {
		t.Errorf("Expected depth limit error, got %v", err)
	}
}

func TestGenericScan_Errors(t *testing.T) {
	mv := &mockScannerVFS{}

	t.Run("Stat error", func(t *testing.T) {
		mv.err = fmt.Errorf("stat failed")
		_, err := GenericScan(context.Background(), mv, "/", []string{"badfile"}, nil)
		if err == nil || err.Error() != "stat failed" {
			t.Errorf("Expected stat error, got %v", err)
		}
	})

	t.Run("ReadDir error", func(t *testing.T) {
		mv.err = nil // Stat works
		mv.readDirErr = fmt.Errorf("readdir failed")
		// Simulate a directory
		mv.onStat = func(p string) VFSItem {
			return VFSItem{Name: "dir", IsDir: true}
		}

		_, err := GenericScan(context.Background(), mv, "/", []string{"dir"}, nil)
		if err == nil || err.Error() != "readdir failed" {
			t.Errorf("Expected readdir error, got %v", err)
		}
	})
}

// --- Additional Mocks ---

type mockScannerVFS struct {
	OSVFS
	err        error
	readDirErr error
	onStat     func(string) VFSItem
	onReadDir  func(string) []VFSItem
}

func (m *mockScannerVFS) Stat(ctx context.Context, p string) (VFSItem, error) {
	if m.err != nil { return VFSItem{}, m.err }
	if m.onStat != nil { return m.onStat(p), nil }
	return VFSItem{Name: "item", IsDir: false}, nil
}

func (m *mockScannerVFS) ReadDir(ctx context.Context, p string, onChunk func([]VFSItem)) error {
	if m.readDirErr != nil { return m.readDirErr }
	if m.onReadDir != nil {
		onChunk(m.onReadDir(p))
	}
	return nil
}

func (m *mockScannerVFS) Join(e ...string) string { return strings.Join(e, "/") }

// --- Mocks ---

type mockFastVFS struct {
	OSVFS
	scanCalled bool
}

func (m *mockFastVFS) Scan(ctx context.Context, basePath string, names []string, cb ScanCallback) (OpStats, error) {
	m.scanCalled = true
	return OpStats{Bytes: 999, Files: 1, Dirs: 1}, nil
}