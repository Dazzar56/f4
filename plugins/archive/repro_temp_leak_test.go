package archive

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/zip"
)

func TestArchiveVFS_TempFileLeak(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Create a dummy ZIP
	zipPath := filepath.Join(tmpDir, "test_leak.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("file.txt")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("hello world"))
	zw.Close()
	f.Close()

	// 2. Open via ArchiveVFS
	vOuter, err := NewArchiveVFS(&vfs.OSVFS{}, zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer vOuter.Close()

	// 3. Open the file to trigger temp file creation (f4arc-*)
	rc, err := vOuter.Open(context.Background(), vOuter.Join(zipPath, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}

	// Extract the temp path
	var tempFilePath string
	if wrapper, ok := rc.(*vfs.TempFileWrapper); ok {
		tempFilePath = wrapper.TempPath
	} else {
		t.Fatalf("Expected vfs.TempFileWrapper, got %T", rc)
	}

	// Ensure it exists before closing
	if _, err := os.Stat(tempFilePath); os.IsNotExist(err) {
		t.Fatalf("Temp file was not created at expected path: %s", tempFilePath)
	}
	t.Logf("Temp file created successfully at: %s", tempFilePath)

	// 4. Close the file (this should delete it in a fixed version)
	rc.Close()

	// 5. Check if it leaked
	if _, err := os.Stat(tempFilePath); err == nil {
		// Clean it up so we don't actually pollute the user's /tmp during tests
		os.Remove(tempFilePath)
		t.Fatalf("TEST FAILED: Temp file %s was not deleted after Close()! Leak detected.", tempFilePath)
	}

	t.Log("SUCCESS: Temp file was properly deleted.")
}