package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/unxed/f4/piecetable"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

// openMappedEditor opens a local file the way showEditor does for UTF-8 with
// mapping available, and fails if the mapping did not happen — these tests are
// about what the mapped path does, not about the fallback.
func openMappedEditor(t *testing.T, dir, path string) *EditorView {
	t.Helper()

	v := vfs.NewOSVFS(dir)
	f, err := v.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	mapped, err := MapEditorFile(v, f)
	if err != nil {
		t.Fatalf("MapEditorFile: %v", err)
	}

	ev := NewEditorView(piecetable.New(mapped.Bytes()), v, path)
	ev.file = f
	ev.mapped = mapped
	ev.Codepage = 65001
	return ev
}

// TestMappedEditor_SearchAllocatesNothing is the whole point of the mapping:
// with the file itself as the piece table's original buffer, a search pass has
// nothing left to assemble. Without it, the first search over a lazily loaded
// buffer copies the file.
func TestMappedEditor_SearchAllocatesNothing(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	drainPendingTasks()

	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	content := strings.Repeat("the quick brown fox\n", 200000) // ~4 MB
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	ev := openMappedEditor(t, dir, path)
	defer ev.Close()

	// Warm anything the first pass builds once.
	if _, err := ev.searchBuffer(nil, ev.editSession); err != nil {
		t.Fatalf("searchBuffer: %v", err)
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	data, err := ev.searchBuffer(nil, ev.editSession)
	if err != nil {
		t.Fatalf("searchBuffer: %v", err)
	}
	off, _, err := findMatch(data, "quick", true, false, false, false, true, 0)
	if err != nil {
		t.Fatalf("findMatch: %v", err)
	}
	runtime.ReadMemStats(&after)

	if off != 4 {
		t.Errorf("match at %d, want 4", off)
	}
	if len(data) != len(content) {
		t.Fatalf("buffer length = %d, want %d", len(data), len(content))
	}
	if &data[0] != &ev.mapped.Bytes()[0] {
		t.Error("the search buffer is a copy, not the mapping")
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 64*1024 {
		t.Errorf("one search pass allocated %d bytes over a %d byte file", allocated, len(content))
	}
}

// TestMappedEditor_IndexesWithoutAnAsyncBuffer covers the indexer's other
// source: it used to return immediately unless there was a chunk buffer to
// read from, which would have left a mapped file with no line index at all.
func TestMappedEditor_IndexesWithoutAnAsyncBuffer(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	drainPendingTasks()

	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	const lines = 5000
	if err := os.WriteFile(path, []byte(strings.Repeat("a line of text\n", lines)), 0644); err != nil {
		t.Fatal(err)
	}

	ev := openMappedEditor(t, dir, path)
	defer ev.Close()
	if ev.asyncBuf != nil {
		t.Fatal("precondition: a mapped editor has no chunk buffer")
	}

	ev.StartIndexing()

	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for ev.li.LineCount() < lines+1 {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		case <-deadline.C:
			t.Fatalf("timeout: indexed %d of %d lines", ev.li.LineCount(), lines+1)
		}
	}
	if got := ev.li.LineCount(); got != lines+1 {
		t.Errorf("line count = %d, want %d", got, lines+1)
	}
}

// TestMappedEditor_EditsAndSaves keeps the mapping honest about being the
// original buffer only: edits land in the add buffer, and the save reads the
// unchanged pieces back through the mapping.
func TestMappedEditor_EditsAndSaves(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	drainPendingTasks()

	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	if err := os.WriteFile(path, []byte("hello world\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ev := openMappedEditor(t, dir, path)
	defer ev.Close()

	ev.insertTextAtCursor([]byte("XYZ "))
	ev.modified = true

	ev.SaveToFile(nil)
	waitEditorSave(t, ev)
	drainPendingTasks()

	if ev.modified {
		t.Error("editor still marked modified: the save reported failure")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "XYZ hello world\n" {
		t.Errorf("content = %q, want %q", got, "XYZ hello world\n")
	}
	// The save reopens the file, and a mapped editor stays mapped afterwards
	// rather than dropping to the chunk buffer.
	if ev.mapped == nil {
		t.Error("the editor lost its mapping across a save")
	}
	assertNoEditorTempSiblings(t, path)
}
