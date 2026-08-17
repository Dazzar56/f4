//go:build windows

package main

import (
	"syscall"
	"unsafe"

	"github.com/unxed/vtui"
)

// Painting the overlay on Windows cannot go through ANSI: with the winapi
// renderer f4 draws into its own screen buffer while the console the user sees
// after Ctrl+O is the original one, and that buffer is not a VT stream.
// Everything here therefore writes cells with the classic Console API.
var (
	procGetConsoleScreenBufferInfoOverlay = kernel32SimpleExec.NewProc("GetConsoleScreenBufferInfo")
	procSetConsoleCursorPositionOverlay   = kernel32SimpleExec.NewProc("SetConsoleCursorPosition")
	procSetConsoleCursorInfoOverlay       = kernel32SimpleExec.NewProc("SetConsoleCursorInfo")
	procSetConsoleWindowInfoOverlay       = kernel32SimpleExec.NewProc("SetConsoleWindowInfo")
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
	// Matches vtui's real KeyBar palette (palette.go: ColKeyBarNum /
	// ColKeyBarText, "LightGray on DarkGray / DarkGray on Teal"). The overlay
	// used to have these two swapped — light text on cyan for the number,
	// plain gray for the label — which is why it read as a different, off
	// keybar next to the real one instead of a seamless continuation of it.
	overlayAttrNum  = uint16(0x07) // light gray on dark gray — the "N" digit
	overlayAttrText = uint16(0x30) // dark text on teal — the label
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

// pinOverlayWindow re-asserts the console window's rectangle before every
// draw, exactly like vtui's own resetConsoleWindowPos() does on every
// Flush() of f4's own screen buffer (win32_console_windows.go) — the code
// path that, per every screenshot so far, renders correctly under Wine.
// The overlay never did this for the host buffer: it only ever *read*
// GetConsoleScreenBufferInfo and trusted the answer. Under a real Wine
// console the buffer legitimately has scrollback (dwSize taller than
// srWindow — confirmed via wineconsole f4.exe: dwSize=80x150,
// srWindow=25 rows), and if the window rect conhost/wineconsole is tracking
// internally ever drifts from what GetConsoleScreenBufferInfo reports back
// (a caching or repaint-ordering gap, not something visible from outside),
// passively trusting the read is exactly the class of bug that produces
// "the numbers say bottom, the pixels say top." Pinning the window to the
// same rectangle we just read is a no-op if Wine's internal state already
// agrees with it, and forces a resync if it does not.
func pinOverlayWindow(h syscall.Handle, w *simpleSmallRect) {
	rect := *w
	procSetConsoleWindowInfoOverlay.Call(uintptr(h), uintptr(1), uintptr(unsafe.Pointer(&rect)))
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
	pinOverlayWindow(h, &info.Window)
	// Re-read after pinning: if the pin forced Wine to reconcile a stale
	// window rect, this is the corrected geometry; if the pin was a no-op,
	// this is identical to what was already read above.
	if info2, ok2 := winOverlayInfo(h); ok2 {
		info = info2
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
	// This is the geometry actually driving the write below, queried by this
	// function itself rather than a separate probe call — if it ever
	// disagrees with the OVERLAY: line's own probe, that gap is the bug.
	// After the pin-and-reread above, so a nonempty diff between this line
	// and the OVERLAY: line one above it in the log means the pin actually
	// changed what Wine reports.
	vtui.DebugLog("OVERLAY_WIN: dwSize=%dx%d srWindow=L%dT%dR%dB%d cmdRow=%d cursor=%d,%d",
		info.Size.X, info.Size.Y, info.Window.Left, info.Window.Top, info.Window.Right, info.Window.Bottom,
		cmdRow, info.CursorPosition.X, info.CursorPosition.Y)

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
			col := fillOverlayText(keyCells, k.Col, k.Num, overlayAttrNum)
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
	pinOverlayWindow(h, &info.Window)
	if info2, ok2 := winOverlayInfo(h); ok2 {
		info = info2
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
