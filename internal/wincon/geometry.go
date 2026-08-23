package wincon

// The console overlay, minus the system calls.
//
// Everything here is arithmetic and judgement, so it compiles and is tested
// everywhere, including on the machine this was written on, which has no
// Windows console at all. The parts that talk to user32 and gdi32 live in
// overlay_windows.go and are the only parts that cannot be.

// Rect is a rectangle in the console window's client coordinates: the pixel
// area the text is drawn in, with (0,0) at its top left corner. That is
// simpler than the X side, where the top of the window may be a menu bar —
// a console window has no furniture inside its client area.
type Rect struct {
	X, Y, W, H int
}

// Source says how the console window was identified, and therefore how much
// it can be trusted.
type Source int

const (
	// SourceNone means no console window was found.
	SourceNone Source = iota

	// SourceConsole means GetConsoleWindow returned a real, visible window:
	// a classic console, which is conhost, which is what cmd.exe runs in.
	SourceConsole

	// SourceHidden means GetConsoleWindow returned a window that is not on
	// the screen. That is what a pseudoconsole looks like — Windows
	// Terminal hosts the console in one and draws the text itself, so the
	// window exists, is never shown, and is the wrong thing to draw over.
	SourceHidden
)

func (s Source) String() string {
	switch s {
	case SourceConsole:
		return "GetConsoleWindow"
	case SourceHidden:
		return "a hidden pseudoconsole"
	}
	return "nothing"
}

// Trusted reports whether the window is one to draw on.
//
// Only a visible console window is. Windows Terminal is deliberately excluded
// and needs no overlay: it renders sixel itself, so pictures go down the wire
// as they do on any capable terminal, and drawing over a window the user never
// sees would put them nowhere.
func (s Source) Trusted() bool { return s == SourceConsole }

// CellRect turns a rectangle of character cells into pixels of the client
// area, given the size of a cell.
//
// The console reports its font size directly, so unlike the terminals on the
// other side of this there is nothing to infer, nothing to round and nothing
// to be a pixel or two out by.
func CellRect(cellW, cellH, c1, r1, c2, r2 int) (Rect, bool) {
	if cellW <= 0 || cellH <= 0 || c2 < c1 || r2 < r1 {
		return Rect{}, false
	}
	return Rect{
		X: c1 * cellW,
		Y: r1 * cellH,
		W: (c2 - c1 + 1) * cellW,
		H: (r2 - r1 + 1) * cellH,
	}, true
}

// ClipToClient trims a rectangle to the client area. A picture is placed from
// the grid, and the grid is what the console says it is, so anything sticking
// out is a disagreement between the two rather than something to draw.
func ClipToClient(r Rect, clientW, clientH int) (Rect, bool) {
	if r.W <= 0 || r.H <= 0 || clientW <= 0 || clientH <= 0 {
		return Rect{}, false
	}
	if r.X < 0 {
		r.W += r.X
		r.X = 0
	}
	if r.Y < 0 {
		r.H += r.Y
		r.Y = 0
	}
	if r.X+r.W > clientW {
		r.W = clientW - r.X
	}
	if r.Y+r.H > clientH {
		r.H = clientH - r.Y
	}
	if r.W <= 0 || r.H <= 0 {
		return Rect{}, false
	}
	return r, true
}

// Union is the smallest rectangle holding all of them, which is the window one
// frame goes into. The gaps between the pictures are cut back out of it with a
// region, so the text between a grid of thumbnails still shows.
func Union(rects []Rect) (Rect, bool) {
	var out Rect
	first := true
	for _, r := range rects {
		if r.W <= 0 || r.H <= 0 {
			continue
		}
		if first {
			out, first = r, false
			continue
		}
		if r.X < out.X {
			out.W += out.X - r.X
			out.X = r.X
		}
		if r.Y < out.Y {
			out.H += out.Y - r.Y
			out.Y = r.Y
		}
		if r.X+r.W > out.X+out.W {
			out.W = r.X + r.W - out.X
		}
		if r.Y+r.H > out.Y+out.H {
			out.H = r.Y + r.H - out.Y
		}
	}
	return out, !first
}

// blitInto copies one picture into the frame buffer at an offset, clipped to
// it. The buffer is BGRA, bottom-up, because that is what a device independent
// bitmap is unless it is told otherwise; the row is chosen by the caller.
func blitInto(dst []byte, dstW, dstH int, src []byte, srcW, srcH, srcStride, atX, atY int) {
	for y := 0; y < srcH; y++ {
		dy := atY + y
		if dy < 0 || dy >= dstH {
			continue
		}
		row := (dstH - 1 - dy) * dstW * 4
		for x := 0; x < srcW; x++ {
			dx := atX + x
			if dx < 0 || dx >= dstW {
				continue
			}
			s := y*srcStride + x*4
			d := row + dx*4
			// RGBA in, BGRA out.
			dst[d+0] = src[s+2]
			dst[d+1] = src[s+1]
			dst[d+2] = src[s+0]
			dst[d+3] = src[s+3]
		}
	}
}
