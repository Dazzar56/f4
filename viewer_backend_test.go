package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/unxed/f4/piecetable"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

func TestViewerBackend_ReadAndFindLineStart(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "test.txt")
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
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
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

// indexingVFS answers line index questions the way a remote file system
// does, and counts the questions so the caching can be checked.
type indexingVFS struct {
	vfs.VFS
	offsets []int64
	total   int64
	calls   int
	err     error
}

func (v *indexingVFS) LineIndex(ctx context.Context, path string, first, count int64) (vfs.LineIndexResult, error) {
	v.calls++
	if v.err != nil {
		return vfs.LineIndexResult{}, v.err
	}
	res := vfs.LineIndexResult{First: first, Total: v.total}
	for i := first; i < first+count && i >= 1 && i <= int64(len(v.offsets)); i++ {
		res.Offsets = append(res.Offsets, v.offsets[i-1])
	}
	return res, nil
}

func TestViewerBackendLineStartFromEnd(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "test.txt")
	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	v := &indexingVFS{VFS: vfs.NewOSVFS(dir), offsets: []int64{0, 6, 12}, total: 3}
	vb, err := NewViewerBackend(context.Background(), v, tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer vb.Close()

	off, ok := vb.LineStartFromEnd(context.Background(), 2)
	if !ok {
		t.Fatal("the indexer was not used")
	}
	if off != 6 {
		t.Errorf("last two lines start at %d, want 6", off)
	}
	if v.calls != 2 {
		t.Errorf("%d index calls, want 2 (total, then offset)", v.calls)
	}

	// The total is already known for this size, so only the offset is asked
	// for the second time round.
	v.calls = 0
	if off, ok = vb.LineStartFromEnd(context.Background(), 1); !ok || off != 12 {
		t.Errorf("second call returned %d, %v", off, ok)
	}
	if v.calls != 1 {
		t.Errorf("%d index calls after caching the total, want 1", v.calls)
	}

	// Asking for more lines than the file has starts at its beginning.
	if off, ok = vb.LineStartFromEnd(context.Background(), 100); !ok || off != 0 {
		t.Errorf("whole file request returned %d, %v", off, ok)
	}
}

func TestViewerBackendLineStartFromEndFallsBack(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(tmp, []byte("a\nb\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// A plain local file system does not implement the interface at all.
	plain, err := NewViewerBackend(context.Background(), vfs.NewOSVFS(dir), tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	if _, ok := plain.LineStartFromEnd(context.Background(), 1); ok {
		t.Error("a file system without the interface claimed to have an index")
	}

	// One that does, but fails, must not be trusted either.
	failing := &indexingVFS{VFS: vfs.NewOSVFS(dir), err: fmt.Errorf("no awk on remote host")}
	vb, err := NewViewerBackend(context.Background(), failing, tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer vb.Close()
	if _, ok := vb.LineStartFromEnd(context.Background(), 1); ok {
		t.Error("a failing index was treated as an answer")
	}
}
func TestViewerBackendLineStart(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "test.txt")
	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		line int64
		want int64
		ok   bool
	}{
		{0, 0, true}, {1, 0, true}, {2, 6, true}, {3, 12, true}, {4, 0, false}, {99, 0, false},
	}

	// Once through the index the file system provides, once through the scan
	// that a file system without one falls back to. Both have to agree, or
	// the same keystroke would land somewhere else depending on the backend.
	indexed := &indexingVFS{VFS: vfs.NewOSVFS(dir), offsets: []int64{0, 6, 12}, total: 3}
	for name, v := range map[string]vfs.VFS{"indexed": indexed, "scanned": vfs.NewOSVFS(dir)} {
		vb, err := NewViewerBackend(context.Background(), v, tmp)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range cases {
			got, ok := vb.LineStart(context.Background(), tc.line)
			if ok != tc.ok || (ok && got != tc.want) {
				t.Errorf("%s: LineStart(%d) = %d, %v; want %d, %v",
					name, tc.line, got, ok, tc.want, tc.ok)
			}
		}
		vb.Close()
	}
	if indexed.calls == 0 {
		t.Error("the index was never asked")
	}
}

func TestViewerBackendLineStartWithoutTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(tmp, []byte("alpha\nomega"), 0644); err != nil {
		t.Fatal(err)
	}
	vb, err := NewViewerBackend(context.Background(), vfs.NewOSVFS(dir), tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer vb.Close()

	if got, ok := vb.LineStart(context.Background(), 2); !ok || got != 6 {
		t.Errorf("LineStart(2) = %d, %v; want 6, true", got, ok)
	}
	if _, ok := vb.LineStart(context.Background(), 3); ok {
		t.Error("a line past the last one was found")
	}
}
