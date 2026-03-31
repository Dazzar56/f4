package main

import (
	"os"
	"testing"

	"github.com/unxed/f4/vfs"
)

func TestViewerBackend_ReadAndFindLineStart(t *testing.T) {
	tmp := t.TempDir() + "/test.txt"
	content := "line1\nline2\nline3"
	os.WriteFile(tmp, []byte(content), 0644)

	v := vfs.NewOSVFS(t.TempDir())
	vb, err := NewViewerBackend(v, tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer vb.Close()

	if vb.Size() != int64(len(content)) {
		t.Fatalf("Expected size %d, got %d", len(content), vb.Size())
	}

	// ReadAt Test
	data, _ := vb.ReadAt(6, 5) // "line2" starts at offset 6
	if string(data) != "line2" {
		t.Errorf("ReadAt failed: expected 'line2', got '%s'", string(data))
	}

	// FindLineStart Test (offset inside "line2")
	start := vb.FindLineStart(8)
	if start != 6 {
		t.Errorf("FindLineStart failed: expected 6, got %d", start)
	}

	// FindLineStart Test (offset inside "line1" / start of file)
	startZero := vb.FindLineStart(3)
	if startZero != 0 {
		t.Errorf("FindLineStart at file beginning should return 0, got %d", startZero)
	}
}