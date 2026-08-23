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
)

// ProbeHostTextArea asks the terminal how large its text area is. It must be
// called before anything else starts reading standard input.
func ProbeHostTextArea() {
	w, h, ok := queryTextAreaPixels()
	hostTextMu.Lock()
	hostTextW, hostTextH, hostTextKnown = w, h, ok
	hostTextMu.Unlock()
	if ok {
		vtui.DebugLog("HOST_GEOM: the text area is %dx%d pixels", w, h)
	} else {
		vtui.DebugLog("HOST_GEOM: the terminal did not answer CSI 14 t")
	}
}

func hostTextArea() (w, h int, ok bool) {
	hostTextMu.Lock()
	defer hostTextMu.Unlock()
	return hostTextW, hostTextH, hostTextKnown
}

// queryTextAreaPixels sends CSI 14 t and reads the CSI 4 ; height ; width t
// that comes back. A terminal that never answers costs the budget below and
// nothing else.
func queryTextAreaPixels() (int, int, bool) {
	in, out := os.Stdin, os.Stdout
	// The controlling terminal is preferred: standard input and output may
	// be pipes even with a tty attached.
	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		defer tty.Close()
		in, out = tty, tty
	}
	if err := in.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		return 0, 0, false
	}
	defer in.SetReadDeadline(time.Time{})
	if _, err := out.WriteString("\x1b[14t"); err != nil {
		return 0, 0, false
	}

	var sb strings.Builder
	buf := make([]byte, 128)
	for {
		n, err := in.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
			if textAreaResponseComplete(sb.String()) {
				break
			}
		}
		if err != nil {
			break
		}
	}
	return parseTextAreaResponse(sb.String())
}

func textAreaResponseComplete(s string) bool {
	return strings.HasSuffix(s, "t") && strings.Contains(s, "\x1b[4;")
}

// parseTextAreaResponse decodes "\x1b[4;height;width t".
func parseTextAreaResponse(s string) (int, int, bool) {
	i := strings.Index(s, "\x1b[4;")
	if i < 0 {
		return 0, 0, false
	}
	rest := s[i+len("\x1b[4;"):]
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
