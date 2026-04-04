package main

import (
	"testing"
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
