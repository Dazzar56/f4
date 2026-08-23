package main

// Where the character grid actually is inside the terminal's window.
//
// The X server knows where the window is. It does not know that the top of
// that window is a menu bar and the right of it a scroll bar, and dividing the
// window by the number of cells in it therefore gives a cell that is slightly
// too large and an origin that is slightly too high — which is exactly what
// put a picture a row and a bit above the space meant for it in gnome-terminal.
//
// The terminal itself knows. CSI 14 t answers with the size of the text area
// in pixels, and that is the missing measurement: the grid is that size, and
// what is left over is furniture.
//
// It has to be asked before the input reader starts, because afterwards the
// answer is just another escape sequence arriving on standard input and the
// reader eats it. So it is asked once, at startup, and remembered.

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/unxed/f4/internal/ttyx"
	"github.com/unxed/vtui"
)

var (
	hostTextMu    sync.Mutex
	hostTextW     int
	hostTextH     int
	hostTextKnown bool
	hostCellW     int
	hostCellH     int
	hostCellKnown bool
)

// ProbeHostTextArea asks the terminal how large its text area is. It must be
// called before anything else starts reading standard input.
//
// Two questions are asked because terminals differ about which they answer.
// CSI 16 t gives the size of one cell, which multiplied by the size of the
// grid is the text area exactly and owes nothing to padding. CSI 14 t gives
// the text area directly. The first is preferred where both come back; where
// neither does, the caller falls back to treating the window as the grid.
func ProbeHostTextArea() {
	cw, ch, cellOK := queryPixels("\x1b[16t", "\x1b[6;")
	tw, th, areaOK := queryPixels("\x1b[14t", "\x1b[4;")
	vtui.DebugLog("TTYX: CSI 16 t -> cell %dx%d (%v), CSI 14 t -> text area %dx%d (%v)",
		cw, ch, cellOK, tw, th, areaOK)

	hostTextMu.Lock()
	hostCellW, hostCellH, hostCellKnown = cw, ch, cellOK
	hostTextW, hostTextH, hostTextKnown = tw, th, areaOK
	hostTextMu.Unlock()
}

// hostCellSize is the pixel size of one cell as the terminal reported it.
func hostCellSize() (int, int, bool) {
	hostTextMu.Lock()
	defer hostTextMu.Unlock()
	return hostCellW, hostCellH, hostCellKnown
}

func hostTextArea() (w, h int, ok bool) {
	hostTextMu.Lock()
	defer hostTextMu.Unlock()
	return hostTextW, hostTextH, hostTextKnown
}

// queryPixels sends one XTWINOPS question and reads the answer with the given
// prefix. Standard input and output are used rather than /dev/tty: where f4
// runs as a server the terminal is the pair of descriptors the client handed
// over, and /dev/tty in that process is either nothing or somebody else's.
//
// A terminal that never answers costs the budget below and nothing else.
func queryPixels(ask, prefix string) (int, int, bool) {
	in, out := os.Stdin, os.Stdout
	if err := in.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		return 0, 0, false
	}
	defer in.SetReadDeadline(time.Time{})
	if _, err := out.WriteString(ask); err != nil {
		return 0, 0, false
	}

	var sb strings.Builder
	buf := make([]byte, 128)
	for {
		n, err := in.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
			if strings.HasSuffix(sb.String(), "t") && strings.Contains(sb.String(), prefix) {
				break
			}
		}
		if err != nil {
			break
		}
	}
	return parseXTWinOps(sb.String(), prefix)
}

// parseXTWinOps decodes "<prefix>height;width t".
func parseXTWinOps(s, prefix string) (int, int, bool) {
	i := strings.Index(s, prefix)
	if i < 0 {
		return 0, 0, false
	}
	rest := s[i+len(prefix):]
	if j := strings.IndexByte(rest, 't'); j >= 0 {
		rest = rest[:j]
	}
	parts := strings.Split(rest, ";")
	if len(parts) < 2 {
		return 0, 0, false
	}
	h, errH := strconv.Atoi(strings.TrimSpace(parts[0]))
	w, errW := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errH != nil || errW != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

// hostTextSize is the size of the character grid in pixels, worked out from
// whichever question the terminal answered. The cell is preferred because it
// owes nothing to padding: multiplied by the grid it is the text area exactly,
// while the reported text area is whatever the terminal chooses to call one.
func hostTextSize(cols, rows int) (int, int, bool) {
	if cw, ch, ok := hostCellSize(); ok && cols > 0 && rows > 0 {
		return cols * cw, rows * ch, true
	}
	return hostTextArea()
}

// hostGridRect works out where the character grid sits inside the terminal
// window, given where the window is.
//
// With a measured text area the grid is put against the left and the bottom of
// the window. That is where every terminal that has furniture keeps it: a menu
// bar is at the top, a scroll bar is on the right, and neither is ever at the
// bottom left. A terminal with a symmetric border is out by the width of that
// border, which is a pixel or two.
//
// Without a measurement the grid is the whole window, which is what this did
// before it could measure anything, and is right for a terminal with no
// furniture at all.
func hostGridRect(win ttyx.Rect, textW, textH int, known bool) ttyx.Rect {
	if !known || textW <= 0 || textH <= 0 {
		return win
	}
	if textW > win.W {
		textW = win.W
	}
	if textH > win.H {
		textH = win.H
	}
	return ttyx.Rect{
		X: win.X,
		Y: win.Y + (win.H - textH),
		W: textW,
		H: textH,
	}
}
