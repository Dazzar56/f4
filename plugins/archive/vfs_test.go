package archive

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/zip"
)

func TestArchiveVFS_PathSlashes(t *testing.T) {
	v := &ArchiveVFS{
		arcPath:   filepath.FromSlash("C:/path/to/archive.zip"),
		innerPath: "folder/file.txt",
	}

	path := v.GetPath()
	expected := filepath.Join(v.arcPath, filepath.FromSlash(v.innerPath))

	if path != expected {
		t.Errorf("ArchiveVFS.GetPath slashes mismatch.\nGot:      %q\nExpected: %q", path, expected)
	}
}

func TestArchiveVFS_Abs(t *testing.T) {
	arcPath := filepath.FromSlash("/tmp/test.zip")
	v := &ArchiveVFS{
		arcPath:   arcPath,
		innerPath: "folder",
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Relative path inside archive",
			input:    "file.txt",
			expected: "/tmp/test.zip/folder/file.txt",
		},
		{
			name:     "Absolute path (full path with archive)",
			input:    "/tmp/test.zip/other",
			expected: "/tmp/test.zip/other",
		},
		{
			name:     "Root-style path inside archive",
			input:    "/manual/root",
			expected: "/manual/root",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exp := filepath.ToSlash(filepath.Clean(tt.expected))
			got, _ := v.Abs(tt.input)
			if got != exp {
				t.Errorf("ArchiveVFS.Abs(%q): expected %q, got %q", tt.input, exp, got)
			}
		})
	}
}

func TestArchiveVFS_AtomicWrite(t *testing.T) {
	tmp := t.TempDir()
	arcPath := filepath.Join(tmp, "test.zip")

	os.WriteFile(arcPath, []byte("PK\x05\x06\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"), 0644)
	origInfo, _ := os.Stat(arcPath)

	v, err := NewArchiveVFS(&vfs.OSVFS{}, arcPath)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	wc, err := v.Create(context.Background(), v.Join(arcPath, "newfile.txt"))
	if err != nil {
		t.Fatal(err)
	}

	wc.Write([]byte("some data"))

	currentInfo, _ := os.Stat(arcPath)
	if currentInfo.Size() != origInfo.Size() {
		t.Error("Original archive size changed BEFORE Close() - not atomic!")
	}
}

func TestArchiveVFS_TempFileLeak(t *testing.T) {
	tmpDir := t.TempDir()

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

	vOuter, err := NewArchiveVFS(&vfs.OSVFS{}, zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer vOuter.Close()

	rc, err := vOuter.Open(context.Background(), vOuter.Join(zipPath, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}

	var tempFilePath string
	if wrapper, ok := rc.(*vfs.TempFileWrapper); ok {
		tempFilePath = wrapper.TempPath
	} else if wrapper, ok := rc.(interface{ TempPath() string }); ok {
		tempFilePath = wrapper.TempPath()
	} else {
		t.Fatalf("Expected wrapper with TempPath, got %T", rc)
	}

	if _, err := os.Stat(tempFilePath); os.IsNotExist(err) {
		t.Fatalf("Temp file was not created at expected path: %s", tempFilePath)
	}
	t.Logf("Temp file created successfully at: %s", tempFilePath)

	rc.Close()

	if _, err := os.Stat(tempFilePath); err == nil {
		os.Remove(tempFilePath)
		t.Fatalf("TEST FAILED: Temp file %s was not deleted after Close()! Leak detected.", tempFilePath)
	}

	t.Log("SUCCESS: Temp file was properly deleted.")
}
// TestArchiveVFS_DeferredClose verifies that closing the ArchiveVFS is deferred
// while there are active readers or writers (grace period of inactivity).
func TestArchiveVFS_DeferredClose(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "test.zip")

	// Create a zip with multiple files
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)

	w1, err := zw.Create("file1.txt")
	if err != nil {
		t.Fatal(err)
	}
	w1.Write([]byte("content1"))

	w2, err := zw.Create("file2.txt")
	if err != nil {
		t.Fatal(err)
	}
	w2.Write([]byte("content2"))

	zw.Close()
	f.Close()

	// 1. Open the archive VFS
	vArc, err := NewArchiveVFS(&vfs.OSVFS{}, zipPath)
	if err != nil {
		t.Fatal(err)
	}

	// 2. Open the first file (simulates beginning of copy/extraction)
	rc1, err := vArc.Open(context.Background(), vArc.Join(zipPath, "file1.txt"))
	if err != nil {
		vArc.Close()
		t.Fatal(err)
	}
	rc1.Close()

	// 3. Simulate exiting the panel (closing the VFS) while extraction is active
	errClose := vArc.Close()
	if errClose != nil {
		t.Logf("vArc.Close() returned error: %v", errClose)
	}

	// 4. Try to open the second file immediately. It should succeed (grace period active)
	rc2, errRead2 := vArc.Open(context.Background(), vArc.Join(zipPath, "file2.txt"))
	if errRead2 != nil {
		t.Fatalf("BUG: Open file2 failed after VFS Close: %v. Expected to succeed due to active copy grace period.", errRead2)
	}
	rc2.Close()

	// 5. Wait for the inactivity timer to expire and perform cleanup (2-second grace period)
	time.Sleep(2500 * time.Millisecond)

	// 6. Try to open the file again. It should fail now as the VFS has been fully cleaned up.
	_, errRead3 := vArc.Open(context.Background(), vArc.Join(zipPath, "file1.txt"))
	if errRead3 == nil {
		t.Error("VFS failed to perform cleanup after inactivity grace period")
	}
}
