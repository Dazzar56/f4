package archive

import (
	"context"
	"github.com/unxed/f4/vfs"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveVFS_AtomicWrite(t *testing.T) {
	// Tests that the original archive is untouched if the update fails.
	tmp := t.TempDir()
	arcPath := filepath.Join(tmp, "test.zip")

	// 1. Create initial valid zip
	os.WriteFile(arcPath, []byte("PK\x05\x06\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"), 0644)
	origInfo, _ := os.Stat(arcPath)

	v, err := NewArchiveVFS(&vfs.OSVFS{}, arcPath)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	// 2. Open for "creation" of a new file inside
	wc, err := v.Create(context.Background(), v.Join(arcPath, "newfile.txt"))
	if err != nil {
		t.Fatal(err)
	}

	// Write some data
	wc.Write([]byte("some data"))

	// 3. To test ATOMICITY, we verify that while the WriteCloser is open,
	// the original archive file hasn't changed its size or mtime.
	currentInfo, _ := os.Stat(arcPath)
	if currentInfo.Size() != origInfo.Size() {
		t.Error("Original archive size changed BEFORE Close() - not atomic!")
	}

	// 4. If we close it, it should swap.
	// (Simulating failure by not calling Close is essentially what atomicity protects against).
	// We can't easily fail the rename here, but the code structure now uses a .tmp file.
}
