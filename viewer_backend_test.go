package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
	"github.com/unxed/vtui/piecetable"
)

func TestViewerBackend_ReadAndFindLineStart(t *testing.T) {
	tmp := t.TempDir() + "/test.txt"
	content := "line1\nline2\nline3"
	os.WriteFile(tmp, []byte(content), 0644)

	v := vfs.NewOSVFS(t.TempDir())
	vb, err := NewViewerBackend(context.Background(), v, tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer vb.Close()

	if vb.Size() != int64(len(content)) {
		t.Fatalf("Expected size %d, got %d", len(content), vb.Size())
	}

	// ReadAt Test
	vtui.FrameManager.Init(vtui.NewScreenBuf())
	var data []byte
	var errLoop error
	for i := 0; i < 100; i++ {
		data, errLoop = vb.ReadAt(6, 5)
		if errLoop == piecetable.ErrLoading {
			select {
			case task := <-vtui.FrameManager.TaskChan:
				task()
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}
		break
	}

	if string(data) != "line2" {
		t.Errorf("ReadAt failed: expected 'line2', got '%s', err: %v", string(data), errLoop)
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