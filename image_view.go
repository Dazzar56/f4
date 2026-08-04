package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

const (
	imageViewMinZoom = 0.05
	imageViewMaxZoom = 40.0

	// Terminal backends cannot always tell us how big a character cell is.
	// The exact numbers only affect the aspect ratio, so a common default is
	// good enough until the size can be queried.
	imageViewFallbackCellW = 8
	imageViewFallbackCellH = 16
)

var imageViewBackAttr = vtui.SetRGBBoth(0, 0xC0C0C0, 0x101010)

// ImageView shows a single picture full screen.
type ImageView struct {
	vtui.BaseFrame
	topBar *TopBar

	vfs     vfs.VFS
	path    string
	surface *vtui.ImageSurface
	decoder string
	gfxKey  string

	zoom       float64
	panX, panY float64

	OnClose func()
}

// NewImageView loads and decodes the file. Decoding happens here rather than
// lazily so that a failure can still be reported as a normal open error.
func NewImageView(ctx context.Context, v vfs.VFS, path string) (*ImageView, error) {
	// Decoding goes through the pipeline, so that reopening a picture is
	// instant and its neighbours can be prepared in advance.
	res := ImagePipe.LoadSync(ctx, v, path)
	if res.Err != nil {
		return nil, res.Err
	}
	surf, decoder := res.Surface, res.Decoder

	iv := &ImageView{
		vfs:     v,
		path:    path,
		surface: surf,
		decoder: decoder,
		zoom:    1,
	}
	iv.gfxKey = fmt.Sprintf("f4.imageview:%p", iv)

	iv.topBar = NewTopBar(func() string {
		base := path
		if v != nil {
			base = v.Base(path)
		} else {
			base = filepath.Base(path)
		}
		return fmt.Sprintf(" %s │ %dx%d │ %d%% │ %s ",
			base, iv.surface.Width, iv.surface.Height,
			int(iv.zoom*100+0.5), iv.decoder)
	})
	iv.topBar.SetVisible(true)
	iv.SetCanFocus(true)
	iv.SetFocus(true)
	return iv, nil
}

func (iv *ImageView) SetPosition(x1, y1, x2, y2 int) {
	iv.ScreenObject.SetPosition(x1, y1, x2, y2)
	if iv.topBar != nil {
		iv.topBar.SetPosition(x1, y1, x2, y1)
	}
}

func (iv *ImageView) ResizeConsole(w, h int) { iv.SetPosition(0, 0, w-1, h-2) }

// SetZoom applies a new zoom factor, 1 meaning "fit into the window".
func (iv *ImageView) SetZoom(z float64) {
	if z < imageViewMinZoom {
		z = imageViewMinZoom
	}
	if z > imageViewMaxZoom {
		z = imageViewMaxZoom
	}
	iv.zoom = z
}

// Pan moves the visible region by a step of one twentieth of the image.
func (iv *ImageView) Pan(dx, dy int) {
	if !iv.surface.Valid() {
		return
	}
	stepX := float64(iv.surface.Width) / 20
	stepY := float64(iv.surface.Height) / 20
	if stepX < 1 {
		stepX = 1
	}
	if stepY < 1 {
		stepY = 1
	}
	iv.panX += float64(dx) * stepX
	iv.panY += float64(dy) * stepY
	if iv.panX < 0 {
		iv.panX = 0
	}
	if iv.panY < 0 {
		iv.panY = 0
	}
}

func (iv *ImageView) clampPan(visW, visH int) {
	maxX := float64(iv.surface.Width - visW)
	maxY := float64(iv.surface.Height - visH)
	if maxX < 0 {
		maxX = 0
	}
	if maxY < 0 {
		maxY = 0
	}
	if iv.panX > maxX {
		iv.panX = maxX
	}
	if iv.panY > maxY {
		iv.panY = maxY
	}
	if iv.panX < 0 {
		iv.panX = 0
	}
	if iv.panY < 0 {
		iv.panY = 0
	}
}

// placementFor computes where and how the picture should appear. While it
// fits, the placement is centred and shows the whole surface; once it is
// zoomed past the window, the placement fills the window and the source
// rectangle is cropped and panned instead.
func (iv *ImageView) placementFor(scr *vtui.ScreenBuf) (vtui.ImagePlacement, bool) {
	if scr == nil || !iv.surface.Valid() {
		return vtui.ImagePlacement{}, false
	}

	x1, y1, x2, y2 := iv.GetPosition()
	top := y1 + 1
	cols := x2 - x1 + 1
	rows := y2 - top + 1
	if cols <= 0 || rows <= 0 {
		return vtui.ImagePlacement{}, false
	}

	cw, ch := scr.Graphics().CellSize()
	if cw <= 0 || ch <= 0 {
		cw, ch = imageViewFallbackCellW, imageViewFallbackCellH
	}

	boxW := cols * cw
	boxH := rows * ch
	fitW, fitH := vtui.FitInside(iv.surface.Width, iv.surface.Height, boxW, boxH)
	if fitW <= 0 || fitH <= 0 {
		return vtui.ImagePlacement{}, false
	}

	dispW := int(float64(fitW)*iv.zoom + 0.5)
	dispH := int(float64(fitH)*iv.zoom + 0.5)
	if dispW < 1 {
		dispW = 1
	}
	if dispH < 1 {
		dispH = 1
	}

	p := vtui.ImagePlacement{Surface: iv.surface}

	if dispW <= boxW && dispH <= boxH {
		iv.panX, iv.panY = 0, 0
		p.Cols, p.Rows = cellsFor(dispW, cw, cols), cellsFor(dispH, ch, rows)
		p.Col = x1 + (cols-p.Cols)/2
		p.Row = top + (rows-p.Rows)/2
		return p, true
	}

	scale := float64(dispW) / float64(iv.surface.Width)
	if scale <= 0 {
		return vtui.ImagePlacement{}, false
	}

	visW := int(float64(boxW) / scale)
	visH := int(float64(boxH) / scale)
	if visW > iv.surface.Width {
		visW = iv.surface.Width
	}
	if visH > iv.surface.Height {
		visH = iv.surface.Height
	}
	if visW < 1 {
		visW = 1
	}
	if visH < 1 {
		visH = 1
	}
	iv.clampPan(visW, visH)

	shownW := int(float64(visW)*scale + 0.5)
	shownH := int(float64(visH)*scale + 0.5)
	if shownW > boxW {
		shownW = boxW
	}
	if shownH > boxH {
		shownH = boxH
	}

	p.Cols, p.Rows = cellsFor(shownW, cw, cols), cellsFor(shownH, ch, rows)
	p.Col = x1 + (cols-p.Cols)/2
	p.Row = top + (rows-p.Rows)/2
	p.SrcX, p.SrcY = int(iv.panX), int(iv.panY)
	p.SrcW, p.SrcH = visW, visH
	return p, true
}

func cellsFor(pixels, cellSize, limit int) int {
	n := (pixels + cellSize - 1) / cellSize
	if n > limit {
		n = limit
	}
	if n < 1 {
		n = 1
	}
	return n
}

func (iv *ImageView) Show(scr *vtui.ScreenBuf) {
	iv.ScreenObject.Show(scr)
	if iv.topBar != nil {
		iv.topBar.Show(scr)
	}

	x1, y1, x2, y2 := iv.GetPosition()
	top := y1 + 1
	scr.FillRect(x1, top, x2, y2, ' ', imageViewBackAttr)

	p, ok := iv.placementFor(scr)
	if !ok {
		return
	}
	if !scr.SupportsGraphics() {
		msg := "This backend cannot display images."
		x := x1 + (x2-x1+1-len(msg))/2
		if x < x1 {
			x = x1
		}
		scr.Write(x, (top+y2)/2, vtui.StringToCharInfo(msg, imageViewBackAttr))
		return
	}
	scr.Graphics().DrawImage(iv.gfxKey, p)
}

func (iv *ImageView) ProcessKey(e *vtinput.InputEvent) bool {
	if e == nil || !e.KeyDown {
		return false
	}

	switch e.Char {
	case '+', '=':
		iv.SetZoom(iv.zoom * 1.25)
		return true
	case '-', '_':
		iv.SetZoom(iv.zoom / 1.25)
		return true
	case '*':
		iv.SetZoom(1)
		iv.panX, iv.panY = 0, 0
		return true
	}

	switch e.VirtualKeyCode {
	case vtinput.VK_ESCAPE, vtinput.VK_F10:
		iv.Close()
		return true
	case vtinput.VK_LEFT:
		iv.Pan(-1, 0)
		return true
	case vtinput.VK_RIGHT:
		iv.Pan(1, 0)
		return true
	case vtinput.VK_UP:
		iv.Pan(0, -1)
		return true
	case vtinput.VK_DOWN:
		iv.Pan(0, 1)
		return true
	}
	return false
}

func (iv *ImageView) HandleCommand(cmd int, args any) bool {
	if cmd == vtui.CmClose {
		iv.Close()
		return true
	}
	return iv.BaseFrame.HandleCommand(cmd, args)
}

func (iv *ImageView) Close() {
	iv.BaseFrame.Close()
	if iv.OnClose != nil {
		iv.OnClose()
	}
}

func (iv *ImageView) GetKeyLabels() *vtui.KeySet {
	return &vtui.KeySet{
		Normal: vtui.KeyBarLabels{
			"", "", "", "", "", "", "", "", "", "Quit",
		},
	}
}

func (iv *ImageView) GetType() vtui.FrameType { return vtui.TypeUser + 7 }

func (iv *ImageView) GetTitle() string {
	if iv.path != "" {
		return "Image: " + filepath.Base(iv.path)
	}
	return "Image"
}
