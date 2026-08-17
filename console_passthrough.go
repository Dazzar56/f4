package main

import (
	"fmt"
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

// overlayLines returns the number of bottom rows reserved for the f4 overlay (0, 1, or 2).
func (pf *PanelsFrame) overlayLines() int {
	if !AppConfig.ConsoleOverlayUI {
		return 0
	}
	n := 1 // CommandLine
	if pf.showKeyBar {
		n++
	}
	return n
}

// drawHostConsoleOverlay renders the CommandLine and KeyBar directly to the host terminal
// using minimal ANSI escape sequences without involving ScreenBuf.
func (pf *PanelsFrame) drawHostConsoleOverlay() {
	if pf.shellMode != ShellModeHost || !pf.isHostConsoleActive() || pf.overlayLines() == 0 {
		return
	}
	h := pf.lastH
	if h <= 0 {
		return
	}
	n := pf.overlayLines()
	cmdRow := h - n + 1 // 1-based row index

	var sb strings.Builder
	// 1. Save cursor position
	sb.WriteString("\x1b7")

	// 2. Draw CommandLine
	sb.WriteString(fmt.Sprintf("\x1b[%d;1H\x1b[0m\x1b[2K", cmdRow))
	prompt := pf.buildPrompt()
	for _, ci := range prompt {
		if ci.Char != vtui.WideCharFiller {
			sb.WriteRune(rune(ci.Char))
		}
	}
	if pf.cmdLine != nil && pf.cmdLine.Edit != nil {
		sb.WriteString(pf.cmdLine.Edit.GetText())
	}

	// 3. Draw KeyBar if visible
	if pf.showKeyBar && n >= 2 {
		keyRow := h
		sb.WriteString(fmt.Sprintf("\x1b[%d;1H\x1b[0m\x1b[2K", keyRow))
		labels := pf.GetKeyLabels()
		if labels != nil {
			for i := 0; i < 12; i++ {
				num := fmt.Sprintf("%d", i+1)
				lbl := labels.Normal[i]
				if len(lbl) > 5 {
					lbl = lbl[:5]
				}
				sb.WriteString(fmt.Sprintf("\x1b[0;30;46m%s\x1b[0;37;40m%-5s", num, lbl))
			}
		}
	}

	// 4. Restore cursor position and visibility
	sb.WriteString("\x1b[0m\x1b8")
	vtui.WritePassthrough([]byte(sb.String()))
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

	n := pf.overlayLines()
	if n > 0 && pf.lastH > n {
		scrollBottom := pf.lastH - n
		vtui.WritePassthrough([]byte(fmt.Sprintf("\x1b[1;%dr", scrollBottom)))
		pf.drawHostConsoleOverlay()
	}
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
