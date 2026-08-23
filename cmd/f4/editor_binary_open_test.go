package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/unxed/f4/piecetable"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

// NUL headers are binary; cp1251 text (invalid UTF-8 but NUL-free) and
// UTF-16 text (NULs decoded away) are not.
func TestEditorHeaderIsBinary(t *testing.T) {
	if !editorHeaderIsBinary([]byte{0, 0, 0, 0x20, 'f', 't'}, 65001) {
		t.Error("NUL header must be binary")
	}
	cp1251 := []byte{0xCF, 0xF0, 0xE8, 0xE2, 0xE5, 0xF2} // "Привет"
	for _, cp := range []int{65001, 1251} {
		if editorHeaderIsBinary(cp1251, cp) {
			t.Errorf("cp1251 text must not be binary (cp=%d)", cp)
		}
	}
	if editorHeaderIsBinary([]byte{0xFF, 0xFE, 'h', 0, 'i', 0}, 1200) {
		t.Error("UTF-16 text must not be binary")
	}
}

// Hex/decode render by byte offset: no line scan may run, and a pending
// line-based restore is meaningless there.
func TestStartIndexingSkipsHexAndDecodeModes(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	drainPendingTasks()

	pt := piecetable.New([]byte("line one\nline two\n"))
	for _, tc := range []struct {
		mode string
		hex  bool
		deco bool
	}{{"hex", true, false}, {"decode", false, true}} {
		ev := newEditorView(pt, nil, "", false, true)
		ev.HexMode, ev.DecodeMode = tc.hex, tc.deco
		ev.targetLine = 1
		ev.StartIndexing()
		if ev.targetLine != -1 {
			t.Errorf("%s mode must clear a pending target line, got %d", tc.mode, ev.targetLine)
		}
		if ev.indexing || ev.indexIsComplete() {
			t.Errorf("%s mode must not run the line-index scan", tc.mode)
		}
	}
}

// A binary file opened for editing goes straight into hex on the lazy chunked
// path (codepage 65001) without a background scan.
func TestShowEditorBinaryOpensInHex(t *testing.T) {
	t.Cleanup(swapFrameManager(t))
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	drainPendingTasks()

	dir := t.TempDir()
	path := filepath.Join(dir, "sample.bin")
	if err := os.WriteFile(path, []byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 0x0A, 'x'}, 0644); err != nil {
		t.Fatal(err)
	}

	localVFS := vfs.NewOSVFS(dir)
	_ = localVFS.SetPath(dir)
	pf := NewPanelsFrame()
	t.Cleanup(pf.Close)
	for _, panel := range pf.panels {
		if fsp, ok := panel.(*FileSystemPanel); ok {
			if fsp.cancelLoad != nil {
				fsp.cancelLoad()
			}
			fsp.stopLoadingAnimation()
		}
	}
	left := NewFileSystemPanel(0, 0, 40, 20, localVFS)
	right := NewFileSystemPanel(40, 0, 40, 20, localVFS.Clone())
	pf.panels[0] = left
	pf.panels[1] = right
	waitForLoad(t, left)
	waitForLoad(t, right)
	pf.ResizeConsole(120, 60)
	vtui.FrameManager.Push(pf)

	f, err := localVFS.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	showEditor(pf, localVFS, path, f)

	ev, _ := findOpenedEditor(localVFS, path)
	if ev == nil {
		t.Fatal("editor was not opened")
	}
	t.Cleanup(ev.Close)
	if !ev.HexMode || ev.Codepage != 65001 {
		t.Errorf("binary file must open in hex with codepage 65001, got hex=%v cp=%d", ev.HexMode, ev.Codepage)
	}
	if ev.indexing || ev.indexIsComplete() {
		t.Error("binary file in hex mode must not run the line-index scan")
	}
}
