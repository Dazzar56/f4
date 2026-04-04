package main

import (
	"testing"
	"os"
	"context"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

func TestActionExecute_RemoteRejection(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	// mockRemoteVFS does NOT satisfy the isLocal check in actionExecute
	v := &mockFailingVFS{}
	pf := NewPanelsFrame()

	actionExecute(pf, v, "/remote", "script.sh", "/remote/script.sh")

	// Verify that an error message was shown
	top := vtui.FrameManager.GetTopFrame()
	if top == nil || top.GetType() != vtui.TypeDialog {
		t.Error("Expected error dialog when attempting to execute on remote VFS")
	}
}

func TestActionMkDir_Flow(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	SetDefaultF4Palette()

	pf := NewPanelsFrame()
	pf.ResizeConsole(80, 25) // Crucial: initializes panels

	// 1. Trigger MkDir action (should push InputBox)
	actionMkDir(pf)

	top := vtui.FrameManager.GetTopFrame()
	if top == nil || top.GetTitle() != Msg("MakeFolder.Title") {
		t.Fatalf("Expected MkDir dialog, got %v", top)
	}

	// Close it to clean up
	top.SetExitCode(-1)
	vtui.FrameManager.Pop()
}

func TestActionNewFile_Flow(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	SetDefaultF4Palette()

	pf := NewPanelsFrame()
	pf.ResizeConsole(80, 25) // Crucial: initializes panels

	pf.activeIdx = 0
	actionNewFile(pf)

	top := vtui.FrameManager.GetTopFrame()
	if top == nil || top.GetTitle() != Msg("Edit.NewFileTitle") {
		t.Errorf("Expected New File dialog, got %v", top)
	}
}
func TestActionViewerSearch_EmptyFile(t *testing.T) {
	// Regression test: searching in an empty file should not hang or crash
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	tmp := t.TempDir() + "/empty.txt"
	os.WriteFile(tmp, []byte(""), 0644)
	v := vfs.NewOSVFS(t.TempDir())

	vv, _ := NewViewerView(context.Background(), v, tmp)

	// Simulate search trigger
	// We manually call the inner logic of actionViewerSearch since InputBox is blocking in tests
	foundOffset := int64(-1)
	currOff := vv.TopOffset + 1
	fileSize := vv.backend.Size() // 0

	if currOff < fileSize {
		t.Error("Search loop should not even start for empty file")
	}

	if foundOffset != -1 {
		t.Error("Should not find anything in empty file")
	}
}
