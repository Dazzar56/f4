package main

// The console overlay, on the f4 side.
//
// Same shape as the X one: installed as vtui's external graphics renderer, so
// the file viewer, the thumbnail grid, quick view and the pictures a program
// prints into the built-in terminal all arrive here together, once a frame,
// and none of them knows the console has no image protocol.
//
// It runs on conhost — cmd.exe in its own window — and nowhere else. Windows
// Terminal renders sixel itself and gets it that way; drawing over the window
// it never shows would put the pictures nowhere.

import (
	"encoding/binary"
	"strconv"
	"strings"
	"sync"

	"github.com/unxed/f4/internal/wincon"
	"github.com/unxed/vtui"
)

type consoleImageOverlay struct {
	ov *wincon.Overlay

	// key is what was last drawn, so a frame nobody is touching is not
	// rescaled and repainted.
	key string
}

var (
	consoleOverlayMu    sync.Mutex
	consoleOverlayInst  *consoleImageOverlay
	consoleOverlayTried bool
)

// InstallConsoleOverlay puts the overlay in as the screen's graphics renderer
// where the console has no way of showing a picture itself.
//
// At startup rather than on the first picture, because everything that shows
// one asks whether the screen supports graphics before it tries, and the
// answer has to be right by then.
func InstallConsoleOverlay() {
	if !AppConfig.ImageX11Overlay {
		return
	}
	scr := vtui.FrameManager.Screen()
	if scr == nil || scr.Graphics().Protocol() != vtui.GraphicsNone {
		// A console that can show pictures on its own keeps doing that.
		return
	}
	if x := sharedConsoleOverlay(); x != nil {
		scr.Graphics().SetExternalGraphics(x)
		cw, ch, ok := wincon.CellSize()
		if ok {
			vtui.DebugLog("WINCON: the cell is %dx%d pixels", cw, ch)
			scr.Graphics().SetCellSize(cw, ch)
		}
	}
}

func sharedConsoleOverlay() *consoleImageOverlay {
	consoleOverlayMu.Lock()
	defer consoleOverlayMu.Unlock()
	if consoleOverlayTried {
		return consoleOverlayInst
	}
	consoleOverlayTried = true

	_, src := wincon.ConsoleWindow()
	if !src.Trusted() {
		vtui.DebugLog("WINCON: not drawing over %v", src)
		return nil
	}
	ov, err := wincon.New()
	if err != nil {
		vtui.DebugLog("WINCON: %v", err)
		return nil
	}
	vtui.DebugLog("WINCON: overlay ready over the console window")
	consoleOverlayInst = &consoleImageOverlay{ov: ov}
	return consoleOverlayInst
}

// RenderExternal is vtui's whole-frame call.
//
// It runs with the screen locked, so nothing here may ask the screen anything:
// everything needed is in the arguments.
func (c *consoleImageOverlay) RenderExternal(list []vtui.ImagePlacement, cellW, cellH, cols, rows int) {
	if c == nil {
		return
	}
	if len(list) == 0 || cellW <= 0 || cellH <= 0 {
		c.hide()
		return
	}
	clientW, clientH, ok := c.ov.ClientSize()
	if !ok {
		c.hide()
		return
	}

	// Where each picture goes, in the console's client pixels.
	pieces := make([]consolePiece, 0, len(list))
	rects := make([]wincon.Rect, 0, len(list))
	for _, p := range list {
		if !p.Surface.Valid() || p.Cols <= 0 || p.Rows <= 0 {
			continue
		}
		r, ok := wincon.CellRect(cellW, cellH, p.Col, p.Row, p.Col+p.Cols-1, p.Row+p.Rows-1)
		if !ok {
			continue
		}
		r, ok = wincon.ClipToClient(r, clientW, clientH)
		if !ok {
			continue
		}
		pieces = append(pieces, consolePiece{rect: r, p: p})
		rects = append(rects, r)
	}
	if len(pieces) == 0 {
		c.hide()
		return
	}

	frame, ok := wincon.Union(rects)
	if !ok {
		c.hide()
		return
	}

	key := consoleFrameKey(pieces, frame)
	if key == c.key && c.ov.Visible() {
		return
	}
	if err := c.ov.Place(frame); err != nil {
		vtui.DebugLog("WINCON: %v", err)
		c.hide()
		return
	}

	buf := make([]byte, frame.W*frame.H*4)
	bounds := make([]wincon.Rect, 0, len(pieces))
	for _, pc := range pieces {
		sub := pc.rect
		sub.X -= frame.X
		sub.Y -= frame.Y

		src := pc.p.Surface
		sx, sy, sw, sh := pc.p.SrcX, pc.p.SrcY, pc.p.SrcW, pc.p.SrcH
		if sw <= 0 || sh <= 0 {
			sw, sh, sx, sy = src.Width, src.Height, 0, 0
		}
		if sx != 0 || sy != 0 || sw != src.Width || sh != src.Height {
			src = src.Crop(sx, sy, sw, sh)
		}
		scaled := vtui.ScaleSurface(src, sub.W, sub.H)
		if !scaled.Valid() {
			continue
		}
		blitInto(buf, frame.W, frame.H, scaled.Pix, scaled.Width, scaled.Height, scaled.Stride, sub.X, sub.Y)
		bounds = append(bounds, sub)
	}
	if len(bounds) == 0 {
		c.hide()
		return
	}

	// The gaps go back to the console, so the captions between a grid of
	// thumbnails stay readable.
	if len(bounds) == 1 && bounds[0] == (wincon.Rect{X: 0, Y: 0, W: frame.W, H: frame.H}) {
		c.ov.SetBounds(nil)
	} else {
		c.ov.SetBounds(bounds)
	}

	if err := c.ov.Draw(buf, frame.W, frame.H, frame.W*4); err != nil {
		vtui.DebugLog("WINCON: %v", err)
		c.hide()
		return
	}
	c.key = key
}

func (c *consoleImageOverlay) hide() {
	if c == nil {
		return
	}
	c.ov.Hide()
	c.key = ""
}

// consolePiece is one picture and where it goes.
type consolePiece struct {
	rect wincon.Rect
	p    vtui.ImagePlacement
}

// consoleFrameKey is what was last drawn: every picture, where it came from
// and where it went.
func consoleFrameKey(pieces []consolePiece, frame wincon.Rect) string {
	var sb strings.Builder
	add := func(v int) {
		sb.WriteString(strconv.Itoa(v))
		sb.WriteByte(0)
	}
	var hashBytes [8]byte
	add(frame.X)
	add(frame.Y)
	add(frame.W)
	add(frame.H)
	for _, pc := range pieces {
		h := pc.p.Surface.Hash()
		binary.LittleEndian.PutUint64(hashBytes[:], h)
		sb.Write(hashBytes[:])
		add(pc.rect.X)
		add(pc.rect.Y)
		add(pc.rect.W)
		add(pc.rect.H)
		add(pc.p.SrcX)
		add(pc.p.SrcY)
		add(pc.p.SrcW)
		add(pc.p.SrcH)
	}
	return sb.String()
}
