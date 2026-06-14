package archive

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
	} else {
		t.Fatalf("Expected vfs.TempFileWrapper, got %T", rc)
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
