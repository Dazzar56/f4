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

	// imageViewPrefetchRadius is how many pictures on each side are decoded
	// before anybody asks to see them.
	imageViewPrefetchRadius = 2
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

	siblings []string
	index    int

	preview    bool
	loading    bool
	err        error
	loadGen    uint64
	actual     bool
	full       bool
	lastScale  float64
	zoom       float64
	panX, panY float64

	OnClose func()
}

// NewImageView loads and decodes the file. Decoding happens here rather than
// lazily so that a failure can still be reported as a normal open error.
func NewImageView(ctx context.Context, v vfs.VFS, path string) (*ImageView, error) {
	// A file that carries a thumbnail opens at once and sharpens when the
	// megapixels arrive; one that does not is waited for.
	res, ok := ImagePipe.PreviewSync(ctx, v, path)
	if !ok {
		res = ImagePipe.LoadSync(ctx, v, path)
		if res.Err != nil {
			return nil, res.Err
		}
	}
	surf, decoder := res.Surface, res.Decoder

	iv := &ImageView{
		vfs:     v,
		path:    path,
		surface: surf,
		decoder: decoder,
		preview: res.Preview,
		zoom:    1,
	}
	iv.gfxKey = fmt.Sprintf("f4.imageview:%p", iv)

	iv.index = -1
	iv.topBar = NewTopBar(func() string {
		base := iv.path
		if v != nil {
			base = v.Base(iv.path)
		} else {
			base = filepath.Base(iv.path)
		}

		state := iv.decoder
		switch {
		case iv.err != nil:
			state = "error: " + iv.err.Error()
		case iv.loading:
			state += ", loading"
		case iv.preview:
			state += ", preview"
		}

		position := ""
		if iv.index >= 0 && len(iv.siblings) > 1 {
			position = fmt.Sprintf(" │ %d/%d", iv.index+1, len(iv.siblings))
		}

		// The scale of the last frame is the honest one: it already knows
		// how large the window is.
		scale := iv.lastScale
		if scale <= 0 {
			scale = iv.zoom
		}
		return fmt.Sprintf(" %s │ %dx%d │ %d%%%s │ %s ",
			base, iv.surface.Width, iv.surface.Height,
			int(scale*100+0.5), position, state)
	})
	iv.topBar.SetVisible(true)
	iv.SetCanFocus(true)
	iv.SetFocus(true)

	// What is on screen is a stand-in; ask for the real thing.
	iv.loading = res.Preview
	if res.Preview {
		gen := iv.loadGen
		ImagePipe.Load(v, path, func(full ImageResult) {
			iv.accept(gen, full)
		})
	}
	return iv, nil
}

// barHeight is how many rows the title bar takes from the picture.
func (iv *ImageView) barHeight() int {
	if iv.full {
		return 0
	}
	return 1
}

// SetSiblings tells the viewer which pictures stand next to this one, in the
// order the panel shows them.
func (iv *ImageView) SetSiblings(paths []string, index int) {
	iv.siblings = paths
	iv.index = index
	iv.prefetch()
}

// prefetch has the neighbours decoded while nobody is looking at them yet.
func (iv *ImageView) prefetch() {
	if iv.index < 0 || iv.index >= len(iv.siblings) {
		return
	}
	ImagePipe.Prefetch(iv.vfs, ImageNeighbourhood(iv.siblings, iv.index, imageViewPrefetchRadius))
}

// Step walks the siblings. It stops at the ends rather than wrapping around,
// so that it stays obvious where the directory begins and where it ends.
func (iv *ImageView) Step(delta int) {
	if len(iv.siblings) == 0 || iv.index < 0 {
		return
	}
	idx := iv.index + delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(iv.siblings) {
		idx = len(iv.siblings) - 1
	}
	iv.GoTo(idx)
}

// GoTo shows the sibling at the given position.
func (iv *ImageView) GoTo(idx int) {
	if idx < 0 || idx >= len(iv.siblings) || iv.siblings[idx] == iv.path {
		return
	}
	iv.index = idx
	iv.open(iv.siblings[idx])
}

// Reload decodes the file again, for a picture that has changed since it was
// put on screen.
func (iv *ImageView) Reload() {
	ImagePipe.Invalidate(iv.vfs, iv.path)
	iv.open(iv.path)
}

// open puts another picture on screen. One that is decoded already appears
// at once; otherwise the previous picture stays until the new one arrives,
// which is quieter than a flash of empty window.
func (iv *ImageView) open(path string) {
	iv.path = path
	iv.zoom = 1
	iv.panX, iv.panY = 0, 0
	iv.err = nil
	iv.loadGen++
	gen := iv.loadGen
	iv.prefetch()

	if res, ok := ImagePipe.Cached(iv.vfs, path); ok {
		iv.accept(gen, res)
		return
	}

	iv.loading = true
	v := iv.vfs
	vtui.RunAsync(func(ctx *vtui.TaskContext) {
		if res, ok := ImagePipe.PreviewSync(ctx.Context, v, path); ok {
			ctx.RunOnUI(func() { iv.accept(gen, res) })
		}
		res := ImagePipe.LoadSync(ctx.Context, v, path)
		ctx.RunOnUI(func() { iv.accept(gen, res) })
	})
}

// accept takes a result unless the reader has moved on since asking for it.
func (iv *ImageView) accept(gen uint64, res ImageResult) {
	if gen != iv.loadGen {
		return
	}
	if res.Err != nil {
		iv.loading = false
		iv.err = res.Err
		vtui.DebugLog("IMAGE: %s: %v", res.Path, res.Err)
		return
	}
	iv.SetImage(res)
	iv.loading = res.Preview
}

// baseScale is what a zoom of one means: the picture fitted into the window,
// or its own pixels when the actual size is asked for.
func (iv *ImageView) baseScale(boxW, boxH int) float64 {
	if !iv.surface.Valid() || iv.surface.Width <= 0 || iv.actual {
		return 1
	}
	fitW, _ := vtui.FitInside(iv.surface.Width, iv.surface.Height, boxW, boxH)
	if fitW <= 0 {
		return 1
	}
	return float64(fitW) / float64(iv.surface.Width)
}

// ToggleActualSize switches between the window and the picture itself
// deciding how large it is shown.
func (iv *ImageView) ToggleActualSize() {
	iv.actual = !iv.actual
	iv.zoom = 1
	iv.panX, iv.panY = 0, 0
}

// SetImage replaces the picture on screen, keeping the viewer looking at the
// same part of it. Sizes differ between a thumbnail and the picture itself,
// so the panning is measured in the new picture's pixels.
func (iv *ImageView) SetImage(res ImageResult) {
	if res.Surface == nil || !res.Surface.Valid() {
		return
	}
	if iv.surface.Valid() && iv.surface.Width > 0 {
		scale := float64(res.Surface.Width) / float64(iv.surface.Width)
		iv.panX *= scale
		iv.panY *= scale
	}
	iv.surface = res.Surface
	iv.decoder = res.Decoder
	iv.preview = res.Preview
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
	top := y1 + iv.barHeight()
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
	scale := iv.baseScale(boxW, boxH) * iv.zoom
	if scale <= 0 {
		return vtui.ImagePlacement{}, false
	}
	iv.lastScale = scale

	dispW := int(float64(iv.surface.Width)*scale + 0.5)
	dispH := int(float64(iv.surface.Height)*scale + 0.5)
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
		iv.topBar.SetVisible(!iv.full)
		if !iv.full {
			iv.topBar.Show(scr)
		}
	}

	x1, y1, x2, y2 := iv.GetPosition()
	top := y1 + iv.barHeight()
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

	ctrl := (e.ControlKeyState & (vtinput.LeftCtrlPressed | vtinput.RightCtrlPressed)) != 0
	if ctrl {
		if e.VirtualKeyCode == vtinput.VK_R {
			iv.Reload()
			return true
		}
		return false
	}

	switch e.Char {
	case '+', '=', 'e', 'E':
		iv.SetZoom(iv.zoom * 1.25)
		return true
	case '-', '_', 'q', 'Q':
		iv.SetZoom(iv.zoom / 1.25)
		return true
	case '*', '0':
		iv.ToggleActualSize()
		return true
	case ' ':
		iv.Step(1)
		return true
	case 'f', 'F':
		iv.full = !iv.full
		return true
	case 'a', 'A':
		iv.Pan(-1, 0)
		return true
	case 'd', 'D':
		iv.Pan(1, 0)
		return true
	case 'w', 'W':
		iv.Pan(0, -1)
		return true
	case 's', 'S':
		iv.Pan(0, 1)
		return true
	}

	switch e.VirtualKeyCode {
	case vtinput.VK_ESCAPE, vtinput.VK_F10:
		iv.Close()
		return true
	case vtinput.VK_NEXT:
		iv.Step(1)
		return true
	case vtinput.VK_PRIOR, vtinput.VK_BACK:
		iv.Step(-1)
		return true
	case vtinput.VK_HOME:
		iv.GoTo(0)
		return true
	case vtinput.VK_END:
		iv.GoTo(len(iv.siblings) - 1)
		return true
	case vtinput.VK_TAB:
		iv.ToggleActualSize()
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
