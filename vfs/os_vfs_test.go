package vfs

import (
	"context"
	"testing"
)

func TestOSVFS_Mutations(t *testing.T) {
	tmpDir := t.TempDir()
	vfs := NewOSVFS(tmpDir)

	// Test MkDir
	newDirPath := vfs.Join(tmpDir, "new_folder")
	err := vfs.MkDir(context.Background(), newDirPath)
	if err != nil {
		t.Fatalf("MkDir failed: %v", err)
	}

	stat, err := vfs.Stat(context.Background(), newDirPath)
	if err != nil || !stat.IsDir {
		t.Errorf("MkDir did not create a directory properly")
	}

	// Test Create & Open (Write/Read)
	filePath := vfs.Join(newDirPath, "test.txt")
	wc, err := vfs.Create(context.Background(), filePath)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	wc.Write([]byte("VFS Test Data"))
	wc.Close()

	rc, err := vfs.Open(context.Background(), filePath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if rc.Size() != 13 {
		t.Errorf("Expected file size 13, got %d", rc.Size())
	}

	buf := make([]byte, 4)
	n, err := rc.ReadAt(context.Background(), buf, 4)
	rc.Close()
	if err != nil || string(buf[:n]) != "Test" {
		t.Errorf("ReadAt failed. Expected 'Test', got %q", string(buf[:n]))
	}

	// Test Rename
	renamedPath := vfs.Join(newDirPath, "renamed.txt")
	err = vfs.Rename(context.Background(), filePath, renamedPath)
	if err != nil {
		t.Fatalf("Rename failed: %v", err)
	}

	_, err = vfs.Stat(context.Background(), filePath)
	if err == nil {
		t.Error("Old file still exists after rename")
	}

	stat, err = vfs.Stat(context.Background(), renamedPath)
	if err != nil || stat.Name != "renamed.txt" {
		t.Error("Renamed file not found or invalid")
	}

	// Test Remove
	err = vfs.Remove(context.Background(), newDirPath)
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	_, err = vfs.Stat(context.Background(), newDirPath)
	if err == nil {
		t.Error("Directory still exists after Remove")
	}
}

func TestOSVFS_Capabilities(t *testing.T) {
	vfs := NewOSVFS(".")
	caps := vfs.GetCapabilities()

	if !caps.HasRandomAccess || !caps.HasServerSideCopy || !caps.HasServerSideMove {
		t.Error("OSVFS should support RandomAccess, ServerSideCopy, and ServerSideMove")
	}
}