package main

import (
	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

// PanelsFrame is the main frame of the f4 manager, containing left and right panels.
type PanelsFrame struct {
	vtui.ScreenObject
	left      Panel
	right     Panel
	activeIdx int // 0 for left, 1 for right

	done      bool
}

func NewPanelsFrame() *PanelsFrame {
	pf := &PanelsFrame{activeIdx: 0}
	pf.SetHelp("Panels")
	return pf
}

func (pf *PanelsFrame) ResizeConsole(w, h int) {
	panelH := h - 2 // Leave space for command line and status
	leftW := w / 2
	rightW := w - leftW

	if pf.left == nil {
		pf.left = NewFileSystemPanel(0, 0, leftW, panelH, ".")
		pf.right = NewFileSystemPanel(leftW, 0, rightW, panelH, ".")
	} else {
		pf.left.SetPosition(0, 0, leftW-1, panelH-1)
		pf.right.SetPosition(leftW, 0, w-1, panelH-1)

		// Special methods for column adaptation (if it's FileSystemPanel)
		if fsp, ok := pf.left.(*FileSystemPanel); ok { fsp.Resize(leftW, panelH) }
		if fsp, ok := pf.right.(*FileSystemPanel); ok { fsp.Resize(rightW, panelH) }
	}
}

func (pf *PanelsFrame) Show(scr *vtui.ScreenBuf) {
	// Coordinates will need to be updated on resize
	if pf.activeIdx == 0 {
		pf.left.SetFocus(true)
		pf.right.SetFocus(false)
	} else {
		pf.left.SetFocus(false)
		pf.right.SetFocus(true)
	}

	pf.left.Show(scr)
	pf.right.Show(scr)

	// Command line (stub)
	scr.Write(0, scr.Height()-1, vtui.StringToCharInfo(Msg("Panels.Prompt"), vtui.SetRGBFore(0, 0xFFFFFF)))
}

func (pf *PanelsFrame) ProcessKey(e *vtinput.InputEvent) bool {
	if !e.KeyDown { return false }

	// F1 invokes help
	if e.VirtualKeyCode == vtinput.VK_F1 {
		pf.ShowHelp()
		return true
	}

	// Tab switches panels
	if e.VirtualKeyCode == vtinput.VK_TAB {
		pf.activeIdx = 1 - pf.activeIdx
		return true
	}

	if pf.activeIdx == 0 {
		return pf.left.ProcessKey(e)
	}
	return pf.right.ProcessKey(e)
}

func (pf *PanelsFrame) ProcessMouse(e *vtinput.InputEvent) bool {
	// Determine which panel was clicked
	mx, my := int(e.MouseX), int(e.MouseY)

	x1, y1, x2, y2 := pf.left.GetPosition()
	if mx >= x1 && mx <= x2 && my >= y1 && my <= y2 {
		pf.activeIdx = 0
		return pf.left.ProcessMouse(e)
	}

	x1, y1, x2, y2 = pf.right.GetPosition()
	if mx >= x1 && mx <= x2 && my >= y1 && my <= y2 {
		pf.activeIdx = 1
		return pf.right.ProcessMouse(e)
	}

	return false
}

func (pf *PanelsFrame) GetType() vtui.FrameType { return vtui.TypePanels }
func (pf *PanelsFrame) SetExitCode(code int)     { pf.done = true }
func (pf *PanelsFrame) IsDone() bool             { return pf.done }