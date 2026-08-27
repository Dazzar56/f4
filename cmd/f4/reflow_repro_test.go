package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Two field bugs, reproduced on the mocks before either is fixed. Both are
// resize colliding with something, and the second loses data, which is the
// one outcome the whole design exists to prevent.

// printBatches writes the shell's output the way ConPTY does during a long
// command: many batches, each opening with the cursor hide (ESC[?25l) and
// closing with ESC[?25h, one or more lines apiece. No batch is a resize
// repaint -- none carries a size report. The test resizes in the middle.
func (f *fakeConPTY) printBatch(lines ...string) {
	f.mu.Lock()
	var sb strings.Builder
	sb.WriteString("\x1b[?25l")
	fmt.Fprintf(&sb, "\x1b[%d;1H", f.viewRowsLocked()+1)
	for _, line := range lines {
		f.lines = append(f.lines, line)
		rows := wrapAt(line, f.cols)
		for i, row := range rows {
			if i < len(rows)-1 {
				sb.WriteString(padRight(row, f.cols))
				sb.WriteString("\r\n")
				continue
			}
			sb.WriteString(row)
			if len([]rune(row)) < f.cols {
				sb.WriteString("\x1b[K")
			}
			sb.WriteString("\r\n")
		}
	}
	f.trimToHeightLocked()
	sb.WriteString("\x1b[?25h")
	f.mu.Unlock()
	f.out <- []byte(sb.String())
}

// BUG 2 (data loss): a resize while a command is printing must not eat the
// output. On the photo, the middle of a `dir` listing was blanked. The
// absorber must never take a batch that is not a resize repaint.
func TestResizeDuringCommandDoesNotEatOutput(t *testing.T) {
	for _, b := range conptyBuilds {
		t.Run(b.name, func(t *testing.T) {
			h := newReflowHarness(t, winReflowOracle, 80, 20)
			h.pty.b = b
			h.pty.prompt("C:\\>dir")
			h.settle(20 * time.Millisecond)
			dir := []string{}
			for i := 0; i < 40; i++ {
				dir = append(dir, fmt.Sprintf("04.04.2024  16:15   %10d  File_%03d_ZZ.dll", i*1000, i))
			}
			// Print in batches, resizing partway through, exactly as a user
			// dragging the window during `dir` would.
			for i, line := range dir {
				h.pty.printBatch(line)
				if i == 15 {
					h.pf.ResizeConsole(72, 20)
				}
				if i == 25 {
					h.pf.ResizeConsole(80, 20)
				}
				h.settle(6 * time.Millisecond)
			}
			h.settle(200 * time.Millisecond)

			text := h.logicalLines()
			joined := strings.Join(text, "\n")
			for _, line := range dir {
				name := line[strings.Index(line, "File_"):]
				if !strings.Contains(joined, name) {
					t.Fatalf("%s: command output was eaten by a resize: %q missing", b.name, name)
				}
			}
		})
	}
}

// BUG 1 (recoverable): occasionally after a resize only a few bottom rows
// show and history does not come back until the next resize. Reproduced as a
// resize whose repaint arrives split across reads, so the absorber is mid
// frame when the pass would refill.
func TestHistoryComesBackWithoutAnExtraResize(t *testing.T) {
	for _, b := range conptyBuilds {
		t.Run(b.name, func(t *testing.T) {
			h := newReflowHarness(t, winReflowOracle, 100, 25)
			h.pty.b = b
			h.pty.setSplitFrames(true)
			var lines []string
			for i := 0; i < 200; i++ {
				lines = append(lines, fmt.Sprintf("line %03d %s", i, strings.Repeat("x", 80)))
			}
			h.pty.print(lines...)
			h.settle(120 * time.Millisecond)
			before := len(h.logicalLines())

			h.pf.ResizeConsole(70, 25)
			h.settle(150 * time.Millisecond)
			h.pf.ResizeConsole(100, 25)
			h.settle(250 * time.Millisecond)

			after := len(h.logicalLines())
			if after < before {
				t.Fatalf("%s: history did not come back on its own: %d logical lines -> %d",
					b.name, before, after)
			}
			// The viewport must be full, not a few bottom rows.
			rows := 0
			h.pf.termView.mu.Lock()
			for y := 0; y < h.pf.termView.Height; y++ {
				if h.pf.termView.rowHasText(y) {
					rows++
				}
			}
			h.pf.termView.mu.Unlock()
			if rows < h.pf.termView.Height/2 {
				t.Fatalf("%s: only %d of %d rows show after resize", b.name, rows, h.pf.termView.Height)
			}
		})
	}
}

// The photo scenario precisely: a dir listing far longer than the screen, so
// it scrolls the whole time, with resizes interleaved. Every filename that
// was printed must be somewhere in the buffer (viewport or history) at the
// end. This is the strongest form of bug 2.
func TestLongScrollingDirWithResizesLosesNothing(t *testing.T) {
	for _, b := range conptyBuilds {
		t.Run(b.name, func(t *testing.T) {
			h := newReflowHarness(t, winReflowOracle, 80, 15)
			h.pty.b = b
			h.pty.prompt("C:\\>dir")
			h.settle(20 * time.Millisecond)
			var names []string
			for i := 0; i < 120; i++ {
				name := fmt.Sprintf("File_%03d_ZZ.dll", i)
				names = append(names, name)
				h.pty.printBatch("04.04.2024  16:15   " + name)
				if i%17 == 8 {
					h.pf.ResizeConsole(70+i%8, 15)
				}
				h.settle(4 * time.Millisecond)
			}
			h.pf.ResizeConsole(80, 15)
			h.settle(250 * time.Millisecond)
			joined := strings.Join(h.logicalLines(), "\n")
			missing := 0
			for _, name := range names {
				if !strings.Contains(joined, name) {
					missing++
				}
			}
			if missing > 0 {
				t.Fatalf("%s: %d of %d filenames lost during a scrolling dir with resizes",
					b.name, missing, len(names))
			}
		})
	}
}

// BUG 1, the mechanism: a repaint that arrives long after its resize -- ConPTY
// under load during a drag -- must still be recognised as a repaint. The
// absorber that keyed on a 250ms window let it land, and the screen showed
// only the rows ConPTY still held until the next resize re-wrapped again.
func TestLateResizeRepaintIsStillAbsorbed(t *testing.T) {
	for _, b := range conptyBuilds {
		t.Run(b.name, func(t *testing.T) {
			h := newReflowHarness(t, winReflowOracle, 100, 25)
			h.pty.b = b
			h.pty.setFrameDelay(400 * time.Millisecond)
			var lines []string
			for i := 0; i < 200; i++ {
				lines = append(lines, fmt.Sprintf("line %03d %s", i, strings.Repeat("x", 80)))
			}
			h.pty.print(lines...)
			h.settle(120 * time.Millisecond)
			before := h.visibleText()

			h.pf.ResizeConsole(60, 25)
			h.settle(700 * time.Millisecond) // the late frame arrives inside this
			if after := h.visibleText(); after < before {
				t.Fatalf("%s: a late repaint landed and destroyed content: %d -> %d", b.name, before, after)
			}
			rows := 0
			h.pf.termView.mu.Lock()
			for y := 0; y < h.pf.termView.Height; y++ {
				if h.pf.termView.rowHasText(y) {
					rows++
				}
			}
			h.pf.termView.mu.Unlock()
			if rows < h.pf.termView.Height-1 {
				t.Fatalf("%s: only %d of %d rows show after the late frame", b.name, rows, h.pf.termView.Height)
			}
		})
	}
}

// A full-screen program repaints from home exactly like a resize repaint.
// f4 running inside f4's own terminal is the case that must work: it switches
// to the alternate screen, where f4 does not re-wrap at all, so ConPTY's
// repaint is the only thing keeping that screen right.
func TestNestedFullScreenProgramKeepsItsFrames(t *testing.T) {
	h := newReflowHarness(t, winReflowOracle, 80, 20)
	h.pty.prompt("C:\\>f4")
	h.settle(20 * time.Millisecond)

	// The inner program takes the alternate screen and draws.
	h.pty.out <- []byte("\x1b[?1049h")
	h.settle(30 * time.Millisecond)
	h.pf.ResizeConsole(76, 20) // a resize while it is up
	h.settle(30 * time.Millisecond)

	frame := "\x1b[?25l\x1b[H\x1b[7m nested f4 panel \x1b[0m\x1b[K\r\ninner content row\x1b[K\x1b[?25h"
	h.pty.out <- []byte(frame)
	h.settle(150 * time.Millisecond)

	var joined string
	h.pf.termView.mu.Lock()
	rows := h.pf.termView.Lines
	if h.pf.termView.UseAltScreen {
		rows = h.pf.termView.AltLines
	}
	for y := 0; y < h.pf.termView.Height && y < len(rows); y++ {
		joined += reflowRowText(rows[y])
	}
	h.pf.termView.mu.Unlock()
	if !strings.Contains(joined, "nested f4 panel") || !strings.Contains(joined, "inner content row") {
		t.Fatalf("a nested full-screen program lost its frame: %q", joined)
	}
	h.pty.out <- []byte("\x1b[?1049l")
	h.settle(30 * time.Millisecond)
}

// A program that clears the screen and repaints from home on the *main*
// screen (cls, a pager) looks like a resize repaint too. It is only dropped
// when ConPTY actually owes f4 a repaint, so an unprompted one lands.
func TestClsStyleRepaintIsNotDroppedWithoutAResize(t *testing.T) {
	h := newReflowHarness(t, winReflowOracle, 80, 20)
	h.pty.prompt("C:\\>cls")
	h.settle(30 * time.Millisecond)

	h.pty.out <- []byte("\x1b[?25l\x1b[H\x1b[2Jfresh screen after cls\x1b[K\x1b[?25h")
	h.settle(150 * time.Millisecond)

	var joined string
	h.pf.termView.mu.Lock()
	for y := 0; y < h.pf.termView.Height; y++ {
		joined += reflowRowText(h.pf.termView.Lines[y])
	}
	h.pf.termView.mu.Unlock()
	if !strings.Contains(joined, "fresh screen after cls") {
		t.Fatalf("a repaint nobody asked for was dropped: %q", joined)
	}
}

// ConPTY does not promise one frame per read. A single read can carry the
// tail of one thing and the head of the next, and the absorber must take the
// repaint and nothing around it. Each of these lost output in review before
// any field run could.
func TestAbsorbTakesOnlyTheRepaintOutOfACoalescedRead(t *testing.T) {
	repaint := "\x1b[?25l\x1b[8;24;80t\x1b[Hrepaint row\x1b[K\x1b[?25h"
	cases := map[string]string{
		"output after the frame":  repaint + "\x1b[?25l\x1b[5;1HFile_after.dll\x1b[K\x1b[?25h",
		"output before the frame": "\x1b[?25l\x1b[5;1HFile_before.dll\x1b[K\x1b[?25h" + repaint,
		"output on both sides":    "\x1b[?25l\x1b[5;1HFile_before.dll\x1b[K\x1b[?25h" + repaint + "\x1b[?25l\x1b[6;1HFile_after.dll\x1b[K\x1b[?25h",
	}
	for name, chunk := range cases {
		t.Run(name, func(t *testing.T) {
			h := newReflowHarness(t, winReflowOracle, 80, 24)
			h.pty.prompt("C:\\>dir")
			h.settle(20 * time.Millisecond)
			h.pf.reflowOracle.absorbResizeRepaint() // one repaint owed
			h.pty.out <- []byte(chunk)
			h.settle(150 * time.Millisecond)
			joined := strings.Join(h.logicalLines(), "\n")
			for _, want := range []string{"File_before.dll", "File_after.dll"} {
				if strings.Contains(chunk, want) && !strings.Contains(joined, want) {
					t.Fatalf("%s: output around the repaint was eaten: %q missing", name, want)
				}
			}
			if strings.Contains(joined, "repaint row") {
				t.Fatalf("%s: the repaint itself reached the display", name)
			}
		})
	}
}

// A repaint whose close never arrives must not hold the stream forever.
// Nothing in ConPTY's measured behaviour omits the close, which is exactly
// why this is guarded: the cost of being wrong is every byte after it.
func TestAbsorbGivesUpOnAnUnclosedFrame(t *testing.T) {
	old := maxAbsorbBytes
	maxAbsorbBytes = 64 << 10 // the guard, not a megabyte of test traffic
	t.Cleanup(func() { maxAbsorbBytes = old })
	h := newReflowHarness(t, winReflowOracle, 80, 24)
	h.pty.prompt("C:\\>dir")
	h.settle(20 * time.Millisecond)
	h.pf.reflowOracle.absorbResizeRepaint()
	h.pty.out <- []byte("\x1b[?25l\x1b[8;24;80t\x1b[Hrepaint that never closes\x1b[K")
	h.settle(30 * time.Millisecond)
	// Past the give-up guard (maxAbsorbBytes), with no close in sight.
	junk := strings.Repeat("x", 4096)
	for sent := 0; sent <= maxAbsorbBytes; sent += len(junk) {
		h.pty.out <- []byte(junk)
		h.settle(2 * time.Millisecond)
	}
	h.settle(150 * time.Millisecond)
	h.pty.out <- []byte("\x1b[?25l\x1b[3;1HFile_survivor.dll\x1b[K\x1b[?25h")
	h.settle(200 * time.Millisecond)
	if !strings.Contains(strings.Join(h.logicalLines(), "\n"), "File_survivor.dll") {
		t.Fatal("an unclosed frame swallowed the stream for good")
	}
}

// A burst of resizes answered late by a slow ConPTY: every repaint is still
// owed and still dropped, however many. A low clamp on the owed count brought
// bug 1 back under exactly this load.
func TestBurstOfLateRepaintsIsFullyAbsorbed(t *testing.T) {
	h := newReflowHarness(t, winReflowOracle, 100, 25)
	h.pty.setFrameDelay(150 * time.Millisecond)
	var lines []string
	for i := 0; i < 150; i++ {
		lines = append(lines, fmt.Sprintf("line %03d %s", i, strings.Repeat("x", 80)))
	}
	h.pty.print(lines...)
	h.settle(100 * time.Millisecond)
	before := h.visibleText()
	for w := 99; w >= 85; w-- { // fifteen resizes before the first repaint lands
		h.pf.ResizeConsole(w, 25)
	}
	h.settle(600 * time.Millisecond)
	if after := h.visibleText(); after < before {
		t.Fatalf("late repaints landed: %d -> %d characters", before, after)
	}
}

// Output split mid-line across reads, which is how the photo's damage looked
// (a filename without its size column): no chunk boundary may cost a byte.
func TestResizeDuringMidLineSplitOutput(t *testing.T) {
	h := newReflowHarness(t, winReflowOracle, 80, 20)
	h.pty.prompt("C:\\>dir")
	h.settle(20 * time.Millisecond)
	for i := 0; i < 30; i++ {
		// Each line is two reads: the date column, then a resize on one of
		// them, then the size and the name. The row advances like a real
		// listing does -- appended under the last, scrolling when full --
		// rather than overwriting earlier rows, which an earlier version
		// of this test did and blamed on the code.
		h.pty.mu.Lock()
		row := h.pty.viewRowsLocked() + 1
		if row > 20 {
			row = 20
		}
		h.pty.lines = append(h.pty.lines, fmt.Sprintf("04.04.2024  16:15      %8d  File_%03d_ZZ.dll", i*1000, i))
		h.pty.trimToHeightLocked()
		h.pty.mu.Unlock()
		if row == 20 {
			h.pty.out <- []byte("\x1b[20;1H\r\n") // scroll one row, as ConPTY would
		}
		h.pty.out <- []byte(fmt.Sprintf("\x1b[?25l\x1b[%d;1H04.04.2024  16:15      ", row))
		if i == 12 {
			h.pf.ResizeConsole(74, 20)
		}
		h.pty.out <- []byte(fmt.Sprintf("%8d  File_%03d_ZZ.dll\x1b[K\x1b[?25h", i*1000, i))
		h.settle(4 * time.Millisecond)
	}
	h.settle(200 * time.Millisecond)
	joined := strings.Join(h.logicalLines(), "\n")
	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("File_%03d_ZZ.dll", i)
		if !strings.Contains(joined, name) || !strings.Contains(joined, "16:15") {
			t.Fatalf("mid-line split output lost around a resize: %q", name)
		}
	}
}
