package main

import (
	"testing"

	"github.com/unxed/vtui"
)

// TestIssue863OwnTerminalKeepsPTYHeightWhileBusy guards the resize that makes
// bash redraw a prompt over output which did not end in a newline.
func TestIssue863OwnTerminalKeepsPTYHeightWhileBusy(t *testing.T) {
	vtui.SetDefaultPalette()
	t.Cleanup(swapFrameManager(t))
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.shellMode = ShellModeOwn
	pf.showPanels = false
	pf.showKeyBar = true
	pf.pty = &mockPty{}

	pf.ResizeConsole(80, 25)
	idleHeight := pf.termView.Height
	if idleHeight <= 0 {
		t.Fatalf("idle own terminal height = %d, want a positive size", idleHeight)
	}

	pf.executing = true
	pf.ResizeConsole(80, 25)
	if pf.termView.Height != idleHeight {
		t.Fatalf("own terminal changed PTY height while busy: got %d, want %d", pf.termView.Height, idleHeight)
	}
	if pf.termView.Y2 >= pf.cmdLine.Y1 {
		t.Fatalf("own terminal overlaps f4 command line: terminal Y2=%d, command line Y1=%d", pf.termView.Y2, pf.cmdLine.Y1)
	}
}
