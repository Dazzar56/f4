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
