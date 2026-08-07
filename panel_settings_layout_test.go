package main

import (
	"testing"

	"github.com/unxed/vtui"
)

func TestPanelSettings_LayoutValidation(t *testing.T) {
	// Initialize default palette and screen buffer for layout validation
	vtui.SetDefaultPalette()
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(120, 60)
	vtui.FrameManager.Init(scr)

	// Call the function to instantiate and push the Panel Settings dialog
	actionPanelSettings(nil)

	// Retrieve the dialog from the frame manager
	topFrame := vtui.FrameManager.GetTopFrame()
	if topFrame == nil {
		t.Fatal("expected Panel Settings dialog to be pushed onto FrameManager, got nil")
	}

	// Run AssertLayout to reproduce the layout validation failure.
	// This will fail the test and print the exact layout errors.
	vtui.AssertLayout(t, topFrame.(vtui.Container))
}
