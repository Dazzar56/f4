package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// The scrollback bug of #425, end to end: a corner drag through the real
// resize path, against a ConPTY that repaints on every ResizePseudoConsole
// call. The history must come through whole.
//
// Everything that hid this for seven field runs is in the sequence: width
// steps, height-only steps and same-size steps interleaved (6.15, 6.16), each
// one answered by the fake with a frame -- with a size report for a size
// change and without one for a same-size call -- exactly as measured.

// visibleText is the characters f4 holds, history and viewport together,
// ignoring layout. It falls only when content is destroyed.
func (h *reflowHarness) visibleText() int {
	tv := h.pf.termView
	tv.mu.Lock()
	defer tv.mu.Unlock()
	return tv.historyCellsLocked() + tv.viewportCellsLocked()
}

func (h *reflowHarness) logicalLines() []string {
	tv := h.pf.termView
	tv.mu.Lock()
	defer tv.mu.Unlock()
	return reflowSnapshot(tv)
}

// cornerDrag is one recorded mouse drag from the field log of 6.16: the
// outer size f4 saw at each FM_RESIZE, including repeats.
var cornerDrag = [][2]int{
	{119, 29}, {110, 27}, {97, 26}, {80, 24}, {80, 24}, {80, 24}, // same-size repeats
	{61, 22}, {41, 20}, {41, 19}, {41, 19}, {37, 19}, // height-only steps
	{61, 22}, {97, 26}, {97, 28}, {120, 30}, {120, 30},
}

func TestCornerDragKeepsTheScrollback(t *testing.T) {
	for _, b := range conptyBuilds {
		t.Run(b.name, func(t *testing.T) {
			h := newReflowHarness(t, winReflowOracle, 120, 30)
			h.pty.b = b
			// Enough output that most of it is history.
			var lines []string
			for i := 0; i < 200; i++ {
				lines = append(lines, fmt.Sprintf("line %03d %s", i, strings.Repeat("x", 100)))
			}
			h.pty.print(lines...)
			h.settle(150 * time.Millisecond)
			before := h.visibleText()
			beforeLines := h.logicalLines()
			if before == 0 {
				t.Fatal("nothing reached the view")
			}

			for _, sz := range cornerDrag {
				h.pf.ResizeConsole(sz[0], sz[1])
				h.settle(40 * time.Millisecond)
			}
			h.settle(300 * time.Millisecond)

			after := h.visibleText()
			if after < before {
				t.Fatalf("the drag destroyed content: %d characters -> %d", before, after)
			}
			afterLines := h.logicalLines()
			if len(afterLines) < len(beforeLines) {
				t.Fatalf("the drag lost logical lines: %d -> %d", len(beforeLines), len(afterLines))
			}
			for i := range beforeLines {
				if beforeLines[i] != afterLines[i] {
					t.Fatalf("line %d changed across the drag:\n before %q\n after  %q", i, beforeLines[i], afterLines[i])
				}
			}
		})
	}
}

// 6.16: a resize event for the size ConPTY already has must not reach
// ResizePseudoConsole at all -- that call is what provokes the sizeless
// repaint.
func TestSameSizeResizeDoesNotReachConPTY(t *testing.T) {
	h := newReflowHarness(t, winReflowOracle, 120, 30)
	h.pty.print("a line")
	h.settle(50 * time.Millisecond)
	h.pf.ResizeConsole(100, 28)
	h.settle(50 * time.Millisecond)
	n := len(h.pty.snapshotResizes())
	for i := 0; i < 3; i++ {
		h.pf.ResizeConsole(100, 28)
	}
	h.settle(50 * time.Millisecond)
	if got := len(h.pty.snapshotResizes()); got != n {
		t.Fatalf("same-size resize events reached ConPTY: %d extra ResizePseudoConsole calls", got-n)
	}
}

// 6.15: a height-only step is followed by a frame, and that frame must not
// land on the display.
func TestHeightOnlyStepAbsorbsTheFrame(t *testing.T) {
	h := newReflowHarness(t, winReflowOracle, 120, 30)
	for i := 0; i < 60; i++ {
		h.pty.print(fmt.Sprintf("row %02d", i))
	}
	h.settle(100 * time.Millisecond)
	before := h.visibleText()
	h.pf.ResizeConsole(120, 26)
	h.settle(100 * time.Millisecond)
	h.pf.ResizeConsole(120, 30)
	h.settle(200 * time.Millisecond)
	if after := h.visibleText(); after < before {
		t.Fatalf("a height-only step and back lost content: %d -> %d", before, after)
	}
}
