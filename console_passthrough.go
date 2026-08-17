package main

import (
	"strings"

	"github.com/unxed/vtui"
)

// mutedPTY wraps a PtyBackend to silence automated parser and terminal responses
// (such as CPR, DSR, DA, OSC 52, far2l APC) while mirroring in ShellModeHost.
// The host terminal provides real responses, so the internal mirror must stay mute.
// Note: mutedPTY intentionally does NOT implement PtyPixelSizer.
type mutedPTY struct {
	backend PtyBackend
}

func (m mutedPTY) Read(p []byte) (int, error)            { return m.backend.Read(p) }
func (m mutedPTY) Write(p []byte) (int, error)           { return len(p), nil }
func (m mutedPTY) Close() error                          { return m.backend.Close() }
func (m mutedPTY) SetSize(cols, rows int)                { m.backend.SetSize(cols, rows) }
func (m mutedPTY) Wait() error                           { return m.backend.Wait() }
func (m mutedPTY) Run(name string, args ...string) error { return m.backend.Run(name, args...) }
func (m mutedPTY) IsBusy() bool                          { return m.backend.IsBusy() }

func (pf *PanelsFrame) isHostConsoleActive() bool {
	pf.hostConsoleMu.Lock()
	defer pf.hostConsoleMu.Unlock()
	return pf.hostConsoleActive
}

// enterHostConsole switches the physical terminal to the primary screen and activates
// live passthrough of PTY output directly to the host console.
func (pf *PanelsFrame) enterHostConsole() {
	if pf.shellMode != ShellModeHost {
		return
	}
	pf.hostConsoleMu.Lock()
	if pf.hostConsoleActive {
		pf.hostConsoleMu.Unlock()
		return
	}
	pf.hostConsoleActive = true
	pf.hostConsoleMu.Unlock()

	pf.SetBusy(true)
	vtui.SetAltScreen(false)
}

// leaveHostConsole restores the alternate screen buffer and returns visual control to f4 panels.
func (pf *PanelsFrame) leaveHostConsole() {
	if pf.shellMode != ShellModeHost {
		return
	}
	pf.hostConsoleMu.Lock()
	if !pf.hostConsoleActive {
		pf.hostConsoleMu.Unlock()
		return
	}
	pf.hostConsoleActive = false
	pf.hostConsoleMu.Unlock()

	// Protective reset sequence to clean up any terminal modes left by child applications
	var resetSeq strings.Builder
	if pf.termView != nil && pf.termView.UseAltScreen {
		resetSeq.WriteString("\x1b[?1049l")
	}
	resetSeq.WriteString("\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?2004l\x1b[r\x1b[0m\x1b[?25h")
	vtui.WritePassthrough([]byte(resetSeq.String()))

	vtui.SetAltScreen(true)
	if vtui.FrameManager != nil && vtui.FrameManager.Screen() != nil {
		vtui.FrameManager.Screen().HardReset()
	}
	pf.SetBusy(false)
	if vtui.FrameManager != nil {
		vtui.FrameManager.Redraw()
	}
}
