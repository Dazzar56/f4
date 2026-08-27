package main

import (
	"os"
	"strings"
	"sync"
	"time"

	"github.com/unxed/vtui"
)

// Windows reflow: recovering the line structure ConPTY takes away.
//
// ConPTY delivers a wrapped line as rows separated by a hard CRLF, with no
// signal that they were one line (docs/TERMINAL_LEDGER.md P6), and the flag
// that would let a terminal reflow is a no-op on current builds (P5). What it
// does do is reflow its own buffer: on every resize it repaints the viewport
// with every wrapped line rejoined (P7). So the line structure cannot be read
// from the stream, but it can be asked for.
//
// Two ways to recover it ship behind one switch, F4_WIN_REFLOW, so that a
// single tester run compares them (TERMINAL_LEDGER.md §3.3.1):
//
//   - hint: winpty's guess, done in TerminalView.HintWrap -- a row that fills
//     the width and ends in CRLF with no ESC[K before it was wrapped. Wrong
//     once in W lines, when a real line is exactly the width.
//   - oracle: at an idle prompt, resize the pseudoconsole very wide, read the
//     repaint (one row per logical line) into a scratch view, resize back,
//     read that repaint too, and match the two: for every viewport row,
//     whether it continues into the next. Written to the display's WrapFlags.
//     The frames go to the scratch view only; the display never sees them.
//     Hint stays on underneath for the rows that scrolled off before a pass.
//   - probe: oracle without writing anything, logging what it would have
//     written next to what hint had written, so the field says how often the
//     guess is wrong and whether the two assumptions the oracle rests on
//     hold (frames delimited by ?25l/?25h; whether the wide frame shows more
//     than the viewport, i.e. conhost keeps scrollback).
//   - off: none of this; Horizontal Preservation as before.

type winReflowMode int

const (
	winReflowOff winReflowMode = iota
	winReflowHint
	winReflowOracle
	winReflowProbe
)

// winReflowModeFromEnv reads F4_WIN_REFLOW. Unset is off until one mode has
// been confirmed in the field.
func winReflowModeFromEnv() winReflowMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("F4_WIN_REFLOW"))) {
	case "hint":
		return winReflowHint
	case "oracle":
		return winReflowOracle
	case "probe":
		return winReflowProbe
	}
	return winReflowOff
}

func (m winReflowMode) String() string {
	switch m {
	case winReflowHint:
		return "hint"
	case winReflowOracle:
		return "oracle"
	case winReflowProbe:
		return "probe"
	}
	return "off"
}

// oracleWideColumns is the width ConPTY is asked to lay the buffer out at.
// COORD.X is an int16; 4000 leaves every realistic line whole.
const oracleWideColumns = 4000

// oracleFrameTimeout bounds the wait for a repaint frame. A resize that
// changes nothing may produce no frame at all, and a build that does not
// bracket frames in ?25l/?25h cannot signal the end; either way the pass
// proceeds with whatever arrived.
var oracleFrameTimeout = 400 * time.Millisecond

// oracleQuietBefore is how long the stream must have been silent before a
// pass starts, so that no ordinary output is caught by the diversion.
var oracleQuietBefore = 100 * time.Millisecond

// oracleReport receives every log line of a pass; tests capture it.
var oracleReport = func(format string, a ...any) { vtui.DebugLog("REFLOW_ORACLE: "+format, a...) }

// reflowOracle runs the resize oracle for one PanelsFrame.
type reflowOracle struct {
	pf   *PanelsFrame
	mode winReflowMode

	mu      sync.Mutex
	running bool
	// sink receives the PTY bytes while a pass is in flight; nil otherwise.
	sink *AnsiParser
	// frameDone is signalled by the scratch view on ESC[?25h.
	frameDone chan struct{}
	lastByte  time.Time
}

func newReflowOracle(pf *PanelsFrame, mode winReflowMode) *reflowOracle {
	return &reflowOracle{pf: pf, mode: mode}
}

// divert returns the parser that should receive PTY bytes right now: the
// scratch parser during a pass, nil otherwise. The read loop consults it.
func (o *reflowOracle) divert() *AnsiParser {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.lastByte = time.Now()
	return o.sink
}

// noteOutput records that the shell produced output; a pass never starts
// until the stream has been quiet for oracleQuietBefore.
func (o *reflowOracle) noteOutput() {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.lastByte = time.Now()
	o.mu.Unlock()
}

// maybeRun starts a pass if the mode wants one and the moment is right. It
// is called by the cmd session on a settled prompt with no console child --
// the only moment a resize is known to reach nothing but an idle shell. It
// returns at once; the pass runs on its own goroutine.
func (o *reflowOracle) maybeRun(pty PtyBackend, cols, rows int) {
	if o == nil || (o.mode != winReflowOracle && o.mode != winReflowProbe) {
		return
	}
	if pty == nil || cols <= 0 || rows <= 0 || cols >= oracleWideColumns {
		return
	}
	tv := o.pf.termView
	if tv == nil || tv.UseAltScreen {
		return
	}
	o.mu.Lock()
	if o.running {
		o.mu.Unlock()
		return
	}
	o.running = true
	o.mu.Unlock()
	go o.run(pty, cols, rows)
}

// run performs one pass: wide frame, narrow frame, match, stamp.
func (o *reflowOracle) run(pty PtyBackend, cols, rows int) {
	defer func() {
		o.mu.Lock()
		o.running = false
		o.sink = nil
		o.mu.Unlock()
	}()

	// Wait for the stream to go quiet, so the diversion catches nothing but
	// the frames.
	for {
		o.mu.Lock()
		quiet := time.Since(o.lastByte) >= oracleQuietBefore
		o.mu.Unlock()
		if quiet {
			break
		}
		time.Sleep(oracleQuietBefore / 4)
	}

	display := o.pf.termView
	before := display.PromptSnapshot()

	wide := o.capture(pty, oracleWideColumns, rows)
	narrow := o.capture(pty, cols, rows)

	after := display.PromptSnapshot()
	if after != before {
		oracleReport("display changed during the pass (%+v -> %+v); nothing stamped", before, after)
		return
	}

	wideRows := trimTrailingEmpty(wide.view.RowTexts())
	narrowRows := narrow.view.RowTexts()
	displayRows := display.RowTexts()

	oracleReport("wide frame: %d content rows, delimited=%v; narrow frame: delimited=%v; viewport %dx%d",
		len(wideRows), wide.delimited, narrow.delimited, cols, rows)
	if len(wideRows) > rows {
		oracleReport("wide frame holds more rows than the viewport: conhost keeps scrollback under ConPTY")
	}

	// The narrow frame must be the screen the user sees, or the coordinates
	// mean nothing.
	for y := range narrowRows {
		if y < len(displayRows) && narrowRows[y] != displayRows[y] {
			oracleReport("mismatch: narrow frame row %d %q differs from the display %q; nothing stamped", y, narrowRows[y], displayRows[y])
			return
		}
	}

	flags, ok := matchWrappedRows(wideRows, narrowRows)
	if !ok {
		oracleReport("mismatch: the wide and narrow frames do not describe the same text; nothing stamped")
		return
	}

	current := display.WrapFlagsCopy()
	disagree := 0
	for y, wrapped := range flags {
		if y < len(current) && current[y] != wrapped {
			disagree++
			oracleReport("row %d: hint said wrapped=%v, oracle says %v: %q", y, current[y], wrapped, narrowRows[y])
		}
	}
	oracleReport("%d rows examined, %d where hint and oracle disagree", len(flags), disagree)

	if o.mode == winReflowOracle {
		display.SetWrapFlags(flags)
	}
}

type capturedFrame struct {
	view      *TerminalView
	delimited bool // the frame ended with ESC[?25h rather than by timeout
}

// capture resizes the pseudoconsole and collects the repaint that follows
// into a scratch view.
func (o *reflowOracle) capture(pty PtyBackend, cols, rows int) capturedFrame {
	view := NewTerminalView(cols, rows)
	defer view.Close()
	done := make(chan struct{}, 1)
	view.OnCursorShown = func() {
		select {
		case done <- struct{}{}:
		default:
		}
	}
	parser := NewAnsiParser(view, nil)

	o.mu.Lock()
	o.sink = parser
	o.frameDone = done
	o.mu.Unlock()

	pty.SetSize(cols, rows)

	delimited := false
	select {
	case <-done:
		delimited = true
	case <-time.After(oracleFrameTimeout):
	}

	o.mu.Lock()
	o.sink = nil
	o.mu.Unlock()
	return capturedFrame{view: view, delimited: delimited}
}

// matchWrappedRows walks the narrow rows consuming the wide rows' text, and
// reports for each narrow row whether it continues into the next. Full rows
// arrive padded to the width (P6), so trailing blanks are ignored on both
// sides and blanks at a row seam are tolerated. Any other disagreement means
// the frames do not describe the same text, and nothing is stamped.
func matchWrappedRows(wideRows, narrowRows []string) (map[int]bool, bool) {
	flags := map[int]bool{}
	i := 0
	rest := ""
	if len(wideRows) > 0 {
		rest = wideRows[0]
	}
	for y, row := range narrowRows {
		if i >= len(wideRows) {
			if row == "" {
				flags[y] = false
				continue
			}
			return nil, false
		}
		if !strings.HasPrefix(rest, row) {
			trimmed := strings.TrimLeft(rest, " ")
			if !strings.HasPrefix(trimmed, row) {
				return nil, false
			}
			rest = trimmed
		}
		rest = rest[len(row):]
		if strings.TrimSpace(rest) == "" {
			flags[y] = false
			i++
			if i < len(wideRows) {
				rest = wideRows[i]
			} else {
				rest = ""
			}
		} else {
			flags[y] = true
		}
	}
	return flags, true
}

func trimTrailingEmpty(rows []string) []string {
	n := len(rows)
	for n > 0 && rows[n-1] == "" {
		n--
	}
	return rows[:n]
}
