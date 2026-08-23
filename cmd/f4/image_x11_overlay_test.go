package main

import (
	"testing"

	"github.com/unxed/f4/internal/ttyx"
	"github.com/unxed/vtui"
)

func TestOverlayCellRect(t *testing.T) {
	// A window at 100,200 that is 800x600 and holds 80x25 cells: a cell is
	// ten by twenty four.
	term := ttyx.Rect{X: 100, Y: 200, W: 800, H: 600}

	got, ok := overlayCellRect(term, 80, 25, 0, 0, 79, 24)
	if !ok || got != (ttyx.Rect{X: 100, Y: 200, W: 800, H: 600}) {
		t.Errorf("whole grid: got %+v ok=%v", got, ok)
	}

	got, ok = overlayCellRect(term, 80, 25, 4, 2, 5, 3)
	if !ok || got != (ttyx.Rect{X: 140, Y: 248, W: 20, H: 48}) {
		t.Errorf("two by two cells at 4,2: got %+v ok=%v", got, ok)
	}
}

// A frame that reaches past the grid is clamped rather than sent off the side
// of the terminal window.
func TestOverlayCellRectClamps(t *testing.T) {
	term := ttyx.Rect{X: 0, Y: 0, W: 800, H: 600}
	got, ok := overlayCellRect(term, 80, 25, -3, -1, 200, 900)
	if !ok || got != (ttyx.Rect{X: 0, Y: 0, W: 800, H: 600}) {
		t.Errorf("clamped: got %+v ok=%v", got, ok)
	}
}

func TestOverlayCellRectRefusesNonsense(t *testing.T) {
	term := ttyx.Rect{X: 0, Y: 0, W: 800, H: 600}
	cases := []struct {
		name           string
		term           ttyx.Rect
		cols, rows     int
		x1, y1, x2, y2 int
	}{
		{"no cells", term, 0, 25, 0, 0, 1, 1},
		{"no window", ttyx.Rect{}, 80, 25, 0, 0, 1, 1},
		{"inverted", term, 80, 25, 10, 10, 4, 4},
		// A grid finer than the window has cells smaller than a pixel.
		{"cell under a pixel", ttyx.Rect{W: 40, H: 10}, 80, 25, 0, 0, 1, 1},
	}
	for _, c := range cases {
		if _, ok := overlayCellRect(c.term, c.cols, c.rows, c.x1, c.y1, c.x2, c.y2); ok {
			t.Errorf("%s: should have been refused", c.name)
		}
	}
}

// The frame key has to move when anything that changes the picture on screen
// moves, and to stay put when nothing does.
func TestOverlayFrameKey(t *testing.T) {
	surf := vtui.NewImageSurface(4, 4)
	surf.SetPixel(0, 0, 1, 2, 3, 255)
	other := vtui.NewImageSurface(4, 4)
	other.SetPixel(0, 0, 9, 9, 9, 255)

	base := []vtui.ImagePlacement{{Surface: surf, Col: 1, Row: 2, Cols: 3, Rows: 4}}
	rect := ttyx.Rect{X: 1, Y: 2, W: 30, H: 40}
	key := overlayFrameKey(base, rect)
	if key != overlayFrameKey(base, rect) {
		t.Fatal("the same frame in the same place must key the same")
	}

	moved := []vtui.ImagePlacement{{Surface: surf, Col: 5, Row: 2, Cols: 3, Rows: 4}}
	swapped := []vtui.ImagePlacement{{Surface: other, Col: 1, Row: 2, Cols: 3, Rows: 4}}
	grew := append(append([]vtui.ImagePlacement(nil), base...), base[0])

	for name, variant := range map[string][]vtui.ImagePlacement{
		"moved": moved, "different picture": swapped, "one more picture": grew,
	} {
		if overlayFrameKey(variant, rect) == key {
			t.Errorf("%s must key differently", name)
		}
	}
	if overlayFrameKey(base, ttyx.Rect{X: 9, Y: 2, W: 30, H: 40}) == key {
		t.Error("a window somewhere else must key differently")
	}
}

// A grid of thumbnails goes into one window with gaps cut out of it, so every
// picture has to land at its own offset inside the frame buffer.
func TestBlitIntoPlacesAndClips(t *testing.T) {
	dst := make([]byte, 4*4*4)
	src := []byte{1, 2, 3, 4, 5, 6, 7, 8}

	blitInto(dst, 4, 4, src, 2, 1, 8, 1, 1)
	if dst[(1*4+1)*4] != 1 || dst[(1*4+2)*4] != 5 {
		t.Errorf("the pixels did not land at the offset: %v", dst[16:32])
	}

	// Anything past the edge is dropped rather than wrapping onto the next
	// row or running off the end of the buffer.
	blitInto(dst, 4, 4, src, 2, 1, 8, 3, 3)
	if dst[(3*4+3)*4] != 1 {
		t.Error("the pixel inside the buffer must still land")
	}
}

// Every method has to survive being called on a nil overlay, because that is
// what "no X here" looks like to the viewer.
func TestOverlayNilIsSafe(t *testing.T) {
	var x *x11ImageOverlay
	x.hide()
	x.close()
	if x.show(nil, vtui.ImagePlacement{}) {
		t.Error("a nil overlay shows nothing")
	}
}
