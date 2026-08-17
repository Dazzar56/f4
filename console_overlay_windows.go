//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// Painting the overlay on Windows cannot go through ANSI: with the winapi
// renderer f4 draws into its own screen buffer while the console the user sees
// after Ctrl+O is the original one, and that buffer is not a VT stream.
// Everything here therefore writes cells with the classic Console API.
var (
	procGetConsoleScreenBufferInfoOverlay = kernel32SimpleExec.NewProc("GetConsoleScreenBufferInfo")
	procSetConsoleCursorPositionOverlay   = kernel32SimpleExec.NewProc("SetConsoleCursorPosition")
	procSetConsoleCursorInfoOverlay       = kernel32SimpleExec.NewProc("SetConsoleCursorInfo")
)

type overlayCoord struct {
	X int16
	Y int16
}

type overlayCursorInfo struct {
	Size    uint32
	Visible int32
}

type overlayBufferInfo struct {
	Size              overlayCoord
	CursorPosition    overlayCoord
	Attributes        uint16
	Window            simpleSmallRect
	MaximumWindowSize overlayCoord
}

const (
	overlayAttrText = uint16(0x07) // light gray on black
	overlayAttrKey  = uint16(0x30) // black on cyan
)

// The console cursor as the child process left it. The overlay moves the cursor
// into the command line, so the original position has to come back before the
// next command starts printing.
var (
	overlaySavedCursor      overlayCoord
	overlaySavedCursorValid bool
)

func winConsoleOverlayAvailable() bool { return true }

func winOverlayHandle() (syscall.Handle, bool) {
	h, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
	if err != nil || h == 0 || h == syscall.InvalidHandle {
		return 0, false
	}
	return h, true
}

func winOverlayInfo(h syscall.Handle) (overlayBufferInfo, bool) {
	var info overlayBufferInfo
	r1, _, _ := procGetConsoleScreenBufferInfoOverlay.Call(uintptr(h), uintptr(unsafe.Pointer(&info)))
	return info, r1 != 0
}

func winOverlayCoordArg(x, y int16) uintptr {
	return uintptr(uint32(uint16(x)) | uint32(uint16(y))<<16)
}

func newOverlayRow(width int) []simpleCharInfo {
	row := make([]simpleCharInfo, width)
	for i := range row {
		row[i] = simpleCharInfo{UnicodeChar: ' ', Attributes: overlayAttrText}
	}
	return row
}

// fillOverlayText writes s into row starting at col and returns the next column.
func fillOverlayText(row []simpleCharInfo, col int, s string, attr uint16) int {
	for _, r := range s {
		if col >= len(row) {
			break
		}
		ch := uint16('?')
		if r < 0x10000 {
			ch = uint16(r)
		}
		row[col] = simpleCharInfo{UnicodeChar: ch, Attributes: attr}
		col++
	}
	return col
}

func winWriteOverlayRow(h syscall.Handle, left, right, top int16, cells []simpleCharInfo) {
	if len(cells) == 0 {
		return
	}
	region := simpleSmallRect{Left: left, Top: top, Right: right, Bottom: top}
	procWriteConsoleOutputW.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&cells[0])),
		winOverlayCoordArg(int16(len(cells)), 1),
		winOverlayCoordArg(0, 0),
		uintptr(unsafe.Pointer(&region)),
	)
}

// winDrawConsoleOverlay paints the overlay onto the bottom rows of the visible
// console window. Rows are counted from srWindow.Bottom rather than from the
// buffer height: a console buffer is routinely taller than its window, and
// using the buffer would put the command line somewhere off screen.
func winDrawConsoleOverlay(ov consoleOverlayContent) {
	if ov.Lines <= 0 {
		return
	}
	h, ok := winOverlayHandle()
	if !ok {
		return
	}
	info, ok := winOverlayInfo(h)
	if !ok {
		return
	}
	left, right := info.Window.Left, info.Window.Right
	width := int(right-left) + 1
	if width <= 0 {
		return
	}
	cmdRow := info.Window.Bottom - int16(ov.Lines) + 1
	if cmdRow < info.Window.Top {
		return
	}

	if !overlaySavedCursorValid {
		overlaySavedCursor = info.CursorPosition
		overlaySavedCursorValid = true
	}

	cmdCells := newOverlayRow(width)
	fillOverlayText(cmdCells, 0, ov.Cmd, overlayAttrText)
	winWriteOverlayRow(h, left, right, cmdRow, cmdCells)

	if len(ov.Keys) > 0 {
		keyCells := newOverlayRow(width)
		for _, k := range ov.Keys {
			// Each slot knows its own column: slot widths are uneven once the
			// width does not divide by 12, so appending sequentially drifts.
			col := fillOverlayText(keyCells, k.Col, k.Num, overlayAttrKey)
			fillOverlayText(keyCells, col, k.Label, overlayAttrText)
		}
		winWriteOverlayRow(h, left, right, info.Window.Bottom, keyCells)
	}

	cursorX := left + int16(ov.CursorCol)
	if cursorX > right {
		cursorX = right
	}
	procSetConsoleCursorPositionOverlay.Call(uintptr(h), winOverlayCoordArg(cursorX, cmdRow))
	ci := overlayCursorInfo{Size: 25, Visible: 1}
	procSetConsoleCursorInfoOverlay.Call(uintptr(h), uintptr(unsafe.Pointer(&ci)))
}

// winClearConsoleOverlay blanks the reserved rows and restores the cursor the
// child process is expected to continue from.
func winClearConsoleOverlay(n int) {
	if n <= 0 {
		return
	}
	h, ok := winOverlayHandle()
	if !ok {
		return
	}
	info, ok := winOverlayInfo(h)
	if !ok {
		return
	}
	left, right := info.Window.Left, info.Window.Right
	width := int(right-left) + 1
	if width <= 0 {
		return
	}
	blank := newOverlayRow(width)
	for i := 0; i < n; i++ {
		row := info.Window.Bottom - int16(n) + 1 + int16(i)
		if row < info.Window.Top {
			continue
		}
		winWriteOverlayRow(h, left, right, row, blank)
	}
	if overlaySavedCursorValid {
		procSetConsoleCursorPositionOverlay.Call(uintptr(h), winOverlayCoordArg(overlaySavedCursor.X, overlaySavedCursor.Y))
		overlaySavedCursorValid = false
	}
}
