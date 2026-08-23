package ttyx

import (
	"fmt"

	"github.com/jezek/xgb/shape"
	"github.com/jezek/xgb/xproto"
)

// Overlay is a window that sits over the terminal and shows pixels, which is
// how a picture reaches the screen in a terminal that knows no image protocol
// at all.
//
// It is override-redirect, so the window manager neither decorates it, nor
// gives it focus, nor lets the user move it away from the terminal it belongs
// to. Its input region is emptied through the SHAPE extension, so the mouse
// goes straight through it to the terminal underneath and selecting text
// keeps working while a picture is up.
//
// The obvious consequence of an override-redirect window is that it is above
// everything, including the applications the user switches to. Nothing in X
// will hide it; the caller has to, which is what Session.Focused is for.
type Overlay struct {
	s      *Session
	win    xproto.Window
	gc     xproto.Gcontext
	rect   Rect
	mapped bool
	shaped bool
	buf    []byte
}

// NewOverlay creates the window. It is not shown until it is placed.
func (s *Session) NewOverlay() (*Overlay, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return nil, ErrNoDisplay
	}

	win, err := xproto.NewWindowId(s.conn)
	if err != nil {
		return nil, fmt.Errorf("no window id for the overlay: %w", err)
	}
	// The value list follows the order of the mask bits: background pixel,
	// backing store, override redirect, event mask.
	//
	// Backing store is asked for because nothing here runs an event loop:
	// without it the server would expect us to repaint on Expose and the
	// picture would go blank the first time something passed over it. A
	// server is free to ignore the request, which is why the caller is
	// still told to redraw when it redraws everything else.
	mask := uint32(xproto.CwBackPixel | xproto.CwBackingStore |
		xproto.CwOverrideRedirect | xproto.CwEventMask)
	values := []uint32{0, xproto.BackingStoreAlways, 1, uint32(xproto.EventMaskExposure)}
	err = xproto.CreateWindowChecked(s.conn, s.depth, win, s.root,
		0, 0, 1, 1, 0, xproto.WindowClassInputOutput, s.visual, mask, values).Check()
	if err != nil {
		return nil, fmt.Errorf("the overlay window could not be created: %w", err)
	}

	gc, err := xproto.NewGcontextId(s.conn)
	if err != nil {
		xproto.DestroyWindow(s.conn, win)
		return nil, fmt.Errorf("no graphics context for the overlay: %w", err)
	}
	if err := xproto.CreateGCChecked(s.conn, gc, xproto.Drawable(win), 0, nil).Check(); err != nil {
		xproto.DestroyWindow(s.conn, win)
		return nil, fmt.Errorf("the overlay graphics context could not be created: %w", err)
	}

	o := &Overlay{s: s, win: win, gc: gc}
	o.clearInputRegion()
	return o, nil
}

// clearInputRegion makes the window transparent to the mouse. A server
// without the SHAPE extension leaves the overlay swallowing clicks, which is
// a nuisance rather than a failure, so it is not an error.
func (o *Overlay) clearInputRegion() {
	if err := shape.Init(o.s.conn); err != nil {
		return
	}
	err := shape.RectanglesChecked(o.s.conn, shape.SoSet, shape.SkInput,
		xproto.ClipOrderingUnsorted, o.win, 0, 0, nil).Check()
	o.shaped = err == nil
}

// PassesInput reports whether the mouse goes through the overlay.
func (o *Overlay) PassesInput() bool { return o.shaped }

// Place moves the overlay to a rectangle of the screen, raises it and shows
// it. An empty rectangle hides it instead.
func (o *Overlay) Place(r Rect) error {
	o.s.mu.Lock()
	defer o.s.mu.Unlock()
	if o.s.conn == nil {
		return ErrNoDisplay
	}
	if r.W <= 0 || r.H <= 0 {
		o.hide()
		return nil
	}

	if r != o.rect {
		// The value list follows the order of the mask bits: x, y, width,
		// height, stack mode.
		err := xproto.ConfigureWindowChecked(o.s.conn, o.win,
			xproto.ConfigWindowX|xproto.ConfigWindowY|
				xproto.ConfigWindowWidth|xproto.ConfigWindowHeight|
				xproto.ConfigWindowStackMode,
			[]uint32{
				uint32(int32(r.X)), uint32(int32(r.Y)),
				uint32(r.W), uint32(r.H),
				xproto.StackModeAbove,
			}).Check()
		if err != nil {
			return fmt.Errorf("the overlay could not be placed: %w", err)
		}
		o.rect = r
	} else {
		xproto.ConfigureWindow(o.s.conn, o.win, xproto.ConfigWindowStackMode,
			[]uint32{xproto.StackModeAbove})
	}

	if !o.mapped {
		xproto.MapWindow(o.s.conn, o.win)
		o.mapped = true
	}
	return nil
}

// Rect is where the overlay currently is.
func (o *Overlay) Rect() Rect { return o.rect }

// Draw puts straight RGBA pixels into the overlay, at its top left corner.
// The caller has already scaled them: an overlay does no scaling, because
// whoever produced the picture knows better than we do how it should be
// resampled.
func (o *Overlay) Draw(pix []byte, w, h, stride int) error {
	o.s.mu.Lock()
	defer o.s.mu.Unlock()
	if o.s.conn == nil {
		return ErrNoDisplay
	}
	if w <= 0 || h <= 0 || stride < w*4 || len(pix) < (h-1)*stride+w*4 {
		return fmt.Errorf("the pixel buffer does not match %dx%d", w, h)
	}

	lineBytes := w * 4
	// A request carries a length in four byte units; leave room for the
	// PutImage header, which is twenty four bytes plus padding.
	maxReq := int(xproto.Setup(o.s.conn).MaximumRequestLength) * 4
	rows := (maxReq - 32) / lineBytes
	if rows < 1 {
		rows = 1
	}
	if rows > h {
		rows = h
	}
	if len(o.buf) < rows*lineBytes {
		o.buf = make([]byte, rows*lineBytes)
	}

	for y := 0; y < h; y += rows {
		n := rows
		if y+n > h {
			n = h - y
		}
		// X wants the channels the other way round on the little endian
		// true colour visuals every modern server uses, and the fourth
		// byte is padding rather than alpha: an overlay is opaque.
		for row := 0; row < n; row++ {
			src := pix[(y+row)*stride : (y+row)*stride+lineBytes]
			dst := o.buf[row*lineBytes : (row+1)*lineBytes]
			for i := 0; i < lineBytes; i += 4 {
				dst[i], dst[i+1], dst[i+2], dst[i+3] = src[i+2], src[i+1], src[i], 0xFF
			}
		}
		err := xproto.PutImageChecked(o.s.conn, xproto.ImageFormatZPixmap,
			xproto.Drawable(o.win), o.gc,
			uint16(w), uint16(n), 0, int16(y), 0, o.s.depth,
			o.buf[:n*lineBytes]).Check()
		if err != nil {
			return fmt.Errorf("the picture could not be sent: %w", err)
		}
	}
	return nil
}

// Hide takes the overlay off the screen without destroying it, which is what
// happens every time the terminal loses the focus.
func (o *Overlay) Hide() {
	o.s.mu.Lock()
	defer o.s.mu.Unlock()
	o.hide()
}

func (o *Overlay) hide() {
	if o.s.conn == nil || !o.mapped {
		return
	}
	xproto.UnmapWindow(o.s.conn, o.win)
	o.mapped = false
}

// Visible reports whether the overlay is currently on the screen.
func (o *Overlay) Visible() bool { return o.mapped }

// Close destroys the window.
func (o *Overlay) Close() {
	o.s.mu.Lock()
	defer o.s.mu.Unlock()
	if o.s.conn == nil {
		return
	}
	xproto.FreeGC(o.s.conn, o.gc)
	xproto.DestroyWindow(o.s.conn, o.win)
	o.mapped = false
}
