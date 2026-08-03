package main

import (
	"io"
	"testing"

	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

func newImageTestScreen(t *testing.T) *vtui.ScreenBuf {
	t.Helper()
	scr := vtui.NewScreenBuf()
	scr.Writer = io.Discard
	scr.AllocBuf(80, 25)
	scr.Graphics().SetProtocol(vtui.GraphicsKitty)
	scr.Graphics().SetCellSize(8, 16)
	return scr
}

func newTestImageView(t *testing.T, w, h int) *ImageView {
	t.Helper()
	iv := &ImageView{
		path:    "test.png",
		surface: vtui.NewImageSurface(w, h),
		decoder: "test",
		zoom:    1,
		gfxKey:  "test-key",
	}
	iv.ResizeConsole(80, 25)
	return iv
}

func TestImageViewFitsAndCentres(t *testing.T) {
	scr := newImageTestScreen(t)
	iv := newTestImageView(t, 100, 100)

	p, ok := iv.placementFor(scr)
	if !ok {
		t.Fatal("layout failed")
	}
	// The window is 80x24 cells of 8x16, so 640x384 pixels. A square image
	// fits to 384x384, which is 48x24 cells, centred horizontally.
	if p.Cols != 48 || p.Rows != 24 {
		t.Errorf("wrong size %dx%d cells", p.Cols, p.Rows)
	}
	if p.Col != 16 || p.Row != 1 {
		t.Errorf("wrong origin %d,%d", p.Col, p.Row)
	}
	if p.SrcW != 0 || p.SrcH != 0 {
		t.Error("a fitting image must not be cropped")
	}
}

func TestImageViewZoomCropsAndPans(t *testing.T) {
	scr := newImageTestScreen(t)
	iv := newTestImageView(t, 100, 100)
	iv.SetZoom(4)

	p, ok := iv.placementFor(scr)
	if !ok {
		t.Fatal("layout failed")
	}
	if p.SrcW <= 0 || p.SrcW >= 100 || p.SrcH <= 0 || p.SrcH >= 100 {
		t.Fatalf("a zoomed image must show only a part of the source, got %dx%d", p.SrcW, p.SrcH)
	}
	if p.SrcX != 0 || p.SrcY != 0 {
		t.Errorf("panning should start at the origin, got %d,%d", p.SrcX, p.SrcY)
	}

	for i := 0; i < 50; i++ {
		iv.Pan(1, 1)
	}
	p, _ = iv.placementFor(scr)
	if p.SrcX+p.SrcW > 100 || p.SrcY+p.SrcH > 100 {
		t.Errorf("panning ran off the image: %d+%d, %d+%d", p.SrcX, p.SrcW, p.SrcY, p.SrcH)
	}
	if p.SrcX == 0 {
		t.Error("panning had no effect")
	}

	for i := 0; i < 50; i++ {
		iv.Pan(-1, -1)
	}
	p, _ = iv.placementFor(scr)
	if p.SrcX != 0 || p.SrcY != 0 {
		t.Errorf("panning back must reach the origin, got %d,%d", p.SrcX, p.SrcY)
	}
}

func TestImageViewZoomOutClearsThePan(t *testing.T) {
	scr := newImageTestScreen(t)
	iv := newTestImageView(t, 100, 100)
	iv.SetZoom(4)
	iv.Pan(1, 1)
	iv.placementFor(scr)

	iv.SetZoom(1)
	p, _ := iv.placementFor(scr)
	if p.SrcX != 0 || p.SrcY != 0 || p.SrcW != 0 {
		t.Errorf("zooming back to fit must drop the crop, got %+v", p)
	}
}

func TestImageViewZoomLimits(t *testing.T) {
	iv := newTestImageView(t, 10, 10)
	for i := 0; i < 200; i++ {
		iv.SetZoom(iv.zoom * 2)
	}
	if iv.zoom > imageViewMaxZoom {
		t.Errorf("zoom escaped its upper limit: %v", iv.zoom)
	}
	for i := 0; i < 200; i++ {
		iv.SetZoom(iv.zoom / 2)
	}
	if iv.zoom < imageViewMinZoom {
		t.Errorf("zoom escaped its lower limit: %v", iv.zoom)
	}
}

func TestImageViewKeys(t *testing.T) {
	iv := newTestImageView(t, 100, 100)

	press := func(char rune, vk uint16) bool {
		return iv.ProcessKey(&vtinput.InputEvent{KeyDown: true, Char: char, VirtualKeyCode: vk})
	}

	if !press('+', 0) || iv.zoom <= 1 {
		t.Error("plus must zoom in")
	}
	if !press('-', 0) {
		t.Error("minus must be handled")
	}
	if !press('*', 0) || iv.zoom != 1 {
		t.Errorf("star must reset the zoom, got %v", iv.zoom)
	}
	if !press(0, vtinput.VK_RIGHT) || iv.panX <= 0 {
		t.Error("the right arrow must pan")
	}
	if press('q', 0) {
		t.Error("unrelated keys must be left to the rest of the UI")
	}
	if !press(0, vtinput.VK_ESCAPE) || !iv.IsDone() {
		t.Error("escape must close the viewer")
	}
}

func TestImageViewShowDeclaresThePlacement(t *testing.T) {
	scr := newImageTestScreen(t)
	iv := newTestImageView(t, 100, 100)

	scr.Graphics().BeginFrame()
	iv.Show(scr)
	scr.Graphics().EndFrame()

	if scr.Graphics().Len() != 1 {
		t.Fatalf("expected one placement, got %d", scr.Graphics().Len())
	}

	// A frame that is not painted must not leave its picture behind.
	scr.Graphics().BeginFrame()
	scr.Graphics().EndFrame()
	if scr.Graphics().Len() != 0 {
		t.Error("the placement outlived the frame that owned it")
	}
}
