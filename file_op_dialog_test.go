package main

import (
	"testing"
	"github.com/unxed/vtui"
)

func TestFileOpProgressDialog_Layout(t *testing.T) {
	vtui.SetDefaultPalette()
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)

	dlg := NewFileOpProgressDialog(" Test Dialog ")

	// Use vtui's layout validator to ensure no overlaps and proper margins
	vtui.AssertLayout(t, dlg)
}

func TestFileOpProgressDialog_VisibilityModes(t *testing.T) {
	vtui.SetDefaultPalette()
	dlg := NewFileOpProgressDialog(" Test ")

	// 1. Scan Mode
	dlg.UpdateScan("/usr/bin", 150, 10)

	if dlg.pbCurrent.IsVisible() || dlg.pbTotal.IsVisible() || dlg.lblSpeed.IsVisible() {
		t.Error("Progress bars and speed label should be hidden in scan mode")
	}

	if dlg.lblTotal.GetText() != "Found: 150 files, 10 folders" {
		t.Errorf("Scan text mismatch: %s", dlg.lblTotal.GetText())
	}

	// 2. Transfer Mode
	dlg.UpdateTransfer("Copying", "file.txt", 50, "Total: 100MB", 20, "5 MB/s")

	if !dlg.pbCurrent.IsVisible() || !dlg.pbTotal.IsVisible() || !dlg.lblSpeed.IsVisible() {
		t.Error("Progress bars and speed label should be visible in transfer mode")
	}

	if dlg.pbCurrent.Percent != 50 || dlg.pbTotal.Percent != 20 {
		t.Errorf("Percents not updated correctly: Curr=%d, Tot=%d", dlg.pbCurrent.Percent, dlg.pbTotal.Percent)
	}

	if dlg.lblCurrent.GetText() != "Copying: file.txt" {
		t.Errorf("Action text mismatch: %s", dlg.lblCurrent.GetText())
	}
}