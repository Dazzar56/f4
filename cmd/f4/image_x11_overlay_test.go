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

// The cache key has to move when anything that changes the picture on screen
// moves, and to stay put when nothing does.
func TestOverlayKey(t *testing.T) {
	base := overlayKey(1234, 0, 0, 100, 50, ttyx.Rect{X: 1, Y: 2, W: 3, H: 4})
	if base != overlayKey(1234, 0, 0, 100, 50, ttyx.Rect{X: 1, Y: 2, W: 3, H: 4}) {
		t.Fatal("the same picture in the same place must key the same")
	}
	others := []string{
		overlayKey(1235, 0, 0, 100, 50, ttyx.Rect{X: 1, Y: 2, W: 3, H: 4}),
		overlayKey(1234, 1, 0, 100, 50, ttyx.Rect{X: 1, Y: 2, W: 3, H: 4}),
		overlayKey(1234, 0, 0, 101, 50, ttyx.Rect{X: 1, Y: 2, W: 3, H: 4}),
		overlayKey(1234, 0, 0, 100, 50, ttyx.Rect{X: 9, Y: 2, W: 3, H: 4}),
		overlayKey(1234, 0, 0, 100, 50, ttyx.Rect{X: 1, Y: 2, W: 3, H: 5}),
	}
	for i, o := range others {
		if o == base {
			t.Errorf("variant %d keys the same as the original", i)
		}
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
