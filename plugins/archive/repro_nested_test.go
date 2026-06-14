package archive

import (
	"context"
	"io"
	"os"
	"testing"
	"path/filepath"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/zip"
	"github.com/unxed/zipper/archive"
)

func TestArchiveVFS_NestedZip(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Create inner ZIP
	innerZipPath := filepath.Join(tmpDir, "inner.zip")
	innerF, err := os.Create(innerZipPath)
	if err != nil {
		t.Fatal(err)
	}
	innerZw := zip.NewWriter(innerF)
	innerFile, err := innerZw.Create("test.txt")
	if err != nil {
		t.Fatal(err)
	}
	innerFile.Write([]byte("nested file content"))
	innerZw.Close()
	innerF.Close()

	// 2. Create outer ZIP with ZSTD compression containing the inner ZIP
	outerZipPath := filepath.Join(tmpDir, "outer.zip")
	opts := archive.Options{
		Method: "zstd",
		Solid:  true,
		Xattrs: true,
	}
	a, err := archive.NewArchiver(outerZipPath, tmpDir, opts)
	if err != nil {
		t.Fatal(err)
	}

	innerFi, _ := os.Stat(innerZipPath)
	err = a.Archive(context.Background(), map[string]os.FileInfo{
		innerZipPath: innerFi,
	})
	if err != nil {
		t.Fatal(err)
	}
	a.Close()

	// 3. Open outer ZIP
	vOuter, err := NewArchiveVFS(&vfs.OSVFS{}, outerZipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer vOuter.Close()

	// 4. Open Solid.zip (nested inside outer.zip and compressed with ZSTD)
	solidPath := vOuter.Join(outerZipPath, "Solid.zip")
	t.Logf("Opening Solid.zip: %q", solidPath)
	vSolid, err := NewArchiveVFS(vOuter, solidPath)
	if err != nil {
		t.Fatalf("FAILED to open Solid.zip: %v", err)
	}
	defer vSolid.Close()

	// 5. Open inner.zip (nested inside Solid.zip)
	nestedPath := vSolid.Join(solidPath, "inner.zip")
	t.Logf("Opening nested archive VFS: %q", nestedPath)

	vInner, err := NewArchiveVFS(vSolid, nestedPath)
	if err != nil {
		t.Fatalf("FAILED to open nested ZIP (inner.zip): %v", err)
	}
	defer vInner.Close()

	rc, err := vInner.Open(context.Background(), vInner.Join(nestedPath, "test.txt"))
	if err != nil {
		t.Fatalf("FAILED to open file inside nested ZIP: %v", err)
	}
	defer rc.Close()

	data, err := io.ReadAll(ctxReader{rc, context.Background()})
	if err != nil {
		t.Fatalf("FAILED to read content from nested ZIP: %v", err)
	}

	if string(data) != "nested file content" {
		t.Errorf("Content mismatch: expected 'nested file content', got %q", string(data))
	}
}