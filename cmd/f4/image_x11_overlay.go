package main

// The image viewer's last resort. When the terminal knows no image protocol
// at all — far2l's own VT is the case that prompted issue #663 — but the
// session is a local X one, the picture goes into an X window placed over the
// terminal instead of being replaced by an apology.
//
// Far did the same thing on Windows: its picture viewer was a window of its
// own, put over the console.
//
// Two rules keep this from being a menace. The window of the terminal has to
// have been identified rather than guessed, because an override-redirect
// window is drawn over whatever is underneath it and guessing wrong means
// painting over a stranger's application. And it comes down the moment the
// terminal loses the focus, because nothing in X will take it down for us.

import (
	"github.com/unxed/f4/internal/ttyx"
	"github.com/unxed/vtui"
)

// x11ImageOverlay owns the connection and the window for one viewer.
type x11ImageOverlay struct {
	sess *ttyx.Session
	ov   *ttyx.Overlay

	// key is what was last drawn, so that a picture nobody is touching is
	// not rescaled and resent on every frame.
	key string
}

// newX11ImageOverlay connects and creates the window, or returns nil when
// there is nothing to connect to, when the terminal window could not be
// identified, or when the option is off.
func newX11ImageOverlay() *x11ImageOverlay {
	if !AppConfig.ImageX11Overlay {
		return nil
	}
	sess, err := ttyx.Open()
	if err != nil {
		vtui.DebugLog("X11_OVERLAY: no session: %v", err)
		return nil
	}
	if !sess.Source().Trusted() {
		// The window was a guess. Drawing over it would be drawing over
		// whatever the user happened to be looking at.
		vtui.DebugLog("X11_OVERLAY: the terminal window was only guessed (%v), standing down", sess.Source())
		sess.Close()
		return nil
	}
	ov, err := sess.NewOverlay()
	if err != nil {
		vtui.DebugLog("X11_OVERLAY: no overlay window: %v", err)
		sess.Close()
		return nil
	}
	vtui.DebugLog("X11_OVERLAY: window %d found through %v, mouse passes through: %v",
		sess.Window(), sess.Source(), ov.PassesInput())
	return &x11ImageOverlay{sess: sess, ov: ov}
}

func (x *x11ImageOverlay) close() {
	if x == nil {
		return
	}
	x.ov.Close()
	x.sess.Close()
}

func (x *x11ImageOverlay) hide() {
	if x == nil {
		return
	}
	x.ov.Hide()
	x.key = ""
}

// overlayCellRect converts a rectangle of character cells into a rectangle of
// screen pixels. The size of a cell is not asked for: it is the size of the
// terminal window divided by the number of cells in it, which is exact when
// the terminal leaves no padding and close enough when it does — and unlike
// CSI 16 t it needs no cooperation from a terminal that has already shown it
// cooperates with nothing.
func overlayCellRect(term ttyx.Rect, cols, rows, x1, y1, x2, y2 int) (ttyx.Rect, bool) {
	if cols <= 0 || rows <= 0 || term.W <= 0 || term.H <= 0 {
		return ttyx.Rect{}, false
	}
	if x2 < x1 || y2 < y1 {
		return ttyx.Rect{}, false
	}
	cellW := term.W / cols
	cellH := term.H / rows
	if cellW <= 0 || cellH <= 0 {
		return ttyx.Rect{}, false
	}
	// The frame is clamped to the grid, so that a stale layout cannot send
	// the picture off the side of the window.
	if x1 < 0 {
		x1 = 0
	}
	if y1 < 0 {
		y1 = 0
	}
	if x2 > cols-1 {
		x2 = cols - 1
	}
	if y2 > rows-1 {
		y2 = rows - 1
	}
	if x2 < x1 || y2 < y1 {
		return ttyx.Rect{}, false
	}
	return ttyx.Rect{
		X: term.X + x1*cellW,
		Y: term.Y + y1*cellH,
		W: (x2 - x1 + 1) * cellW,
		H: (y2 - y1 + 1) * cellH,
	}, true
}

// show puts the placement on the screen and reports whether it managed to.
// The caller falls back to its apology when it did not.
func (x *x11ImageOverlay) show(scr *vtui.ScreenBuf, p vtui.ImagePlacement) bool {
	if x == nil || scr == nil || !p.Surface.Valid() {
		return false
	}
	if !x.sess.Focused() {
		// Somebody else is on top of the terminal now, and an
		// override-redirect window would be on top of them.
		x.hide()
		return false
	}

	term, err := x.sess.Geometry()
	if err != nil {
		x.hide()
		return false
	}
	cols, rows := scr.Width(), scr.Height()
	rect, ok := overlayCellRect(term, cols, rows, p.Col, p.Row, p.Col+p.Cols-1, p.Row+p.Rows-1)
	if !ok {
		x.hide()
		return false
	}
	if err := x.ov.Place(rect); err != nil {
		vtui.DebugLog("X11_OVERLAY: %v", err)
		x.hide()
		return false
	}

	sx, sy, sw, sh := p.SrcX, p.SrcY, p.SrcW, p.SrcH
	if sw <= 0 || sh <= 0 {
		sw, sh = p.Surface.Width, p.Surface.Height
		sx, sy = 0, 0
	}
	key := overlayKey(p.Surface.Hash(), sx, sy, sw, sh, rect)
	if key == x.key {
		return true
	}

	src := p.Surface
	if sx != 0 || sy != 0 || sw != src.Width || sh != src.Height {
		src = src.Crop(sx, sy, sw, sh)
	}
	scaled := vtui.ScaleSurface(src, rect.W, rect.H)
	if !scaled.Valid() {
		x.hide()
		return false
	}
	if err := x.ov.Draw(scaled.Pix, scaled.Width, scaled.Height, scaled.Stride); err != nil {
		vtui.DebugLog("X11_OVERLAY: %v", err)
		x.hide()
		return false
	}
	x.key = key
	return true
}

func overlayKey(hash uint64, sx, sy, sw, sh int, r ttyx.Rect) string {
	var buf []byte
	add := func(v int) {
		buf = append(buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	}
	for i := 0; i < 8; i++ {
		buf = append(buf, byte(hash>>(8*i)))
	}
	add(sx)
	add(sy)
	add(sw)
	add(sh)
	add(r.X)
	add(r.Y)
	add(r.W)
	add(r.H)
	return string(buf)
}
