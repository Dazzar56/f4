package main

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
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
//     read that repaint too, and match the two. Exact consecutive pairs are
//     aligned against f4's GridHistory + viewport journal, so confirmed flags
//     also update rows outside the visible grid and survive after ConPTY
//     forgets them. The frames go to scratch views only; the display never
//     sees them. Hint remains for boundaries no oracle pass overlaps.
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

// winReflowModeFromEnv reads F4_WIN_REFLOW. The safe oracle is the Windows
// default: every stamp is checked against both ConPTY frames and the local
// journal before it can change history. Explicit off remains the escape hatch.
func winReflowModeFromEnv() winReflowMode {
	return parseWinReflowMode(os.Getenv("F4_WIN_REFLOW"), runtime.GOOS == "windows")
}

func parseWinReflowMode(value string, windows bool) winReflowMode {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "off":
		return winReflowOff
	case "hint":
		return winReflowHint
	case "oracle":
		return winReflowOracle
	case "probe":
		return winReflowProbe
	}
	if value == "" && windows {
		return winReflowOracle
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
	// absorbArmedUntil is when a resize stops explaining a repaint frame;
	// absorbing is the scratch parser of a frame being taken across chunks.
	absorbArmedUntil time.Time
	absorbing        *AnsiParser
	absorbedFrames   int
	lastByte         time.Time
}

func newReflowOracle(pf *PanelsFrame, mode winReflowMode) *reflowOracle {
	return &reflowOracle{pf: pf, mode: mode}
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
	journal := display.reflowJournalSnapshot()

	oracleReport("wide frame: %d content rows, delimited=%v; narrow frame: delimited=%v; viewport %dx%d",
		len(wideRows), wide.delimited, narrow.delimited, cols, rows)
	if len(wideRows) > rows {
		oracleReport("wide frame holds more rows than the viewport: conhost keeps scrollback under ConPTY")
	}

	flags, ok := matchWrappedRows(wideRows, narrowRows)
	if !ok {
		oracleReport("mismatch: the wide and narrow frames do not describe the same text; nothing stamped")
		return
	}

	// A repaint is ConPTY's viewport, not necessarily f4's viewport. f4 may
	// already have moved its first rows into GridHistory, and its private cmd
	// sync excision intentionally removes rows which remain in ConPTY. Align
	// exact runs against the combined local journal rather than comparing y=y.
	// A boundary is stamped only when both rows around it match consecutively;
	// isolated/duplicate anchors cannot corrupt history.
	alignment := alignReflowRows(narrowRows, journal)
	sourcePairs := make(map[string]int)
	for i := 0; i+1 < len(narrowRows); i++ {
		if narrowRows[i] != "" && narrowRows[i+1] != "" {
			sourcePairs[reflowRowPairKey(narrowRows[i], narrowRows[i+1])]++
		}
	}
	targetPairs := make(map[string]int)
	for i := 0; i+1 < len(journal); i++ {
		if journal[i].text != "" && journal[i+1].text != "" {
			targetPairs[reflowRowPairKey(journal[i].text, journal[i+1].text)]++
		}
	}
	stamps := make(map[int]bool)
	for i := 0; i+1 < len(alignment); i++ {
		a, b := alignment[i], alignment[i+1]
		if b.source == a.source+1 && b.target == a.target+1 {
			key := reflowRowPairKey(narrowRows[a.source], narrowRows[b.source])
			// Repeated identical pairs are not anchors. Choosing one occurrence
			// could stamp a true wrap onto a same-looking hard-broken line.
			if wrapped, exists := flags[a.source]; exists && sourcePairs[key] == 1 && targetPairs[key] == 1 {
				stamps[a.target] = wrapped
			}
		}
	}
	if len(stamps) == 0 {
		oracleReport("mismatch: no consecutive repaint rows occur in the local history+viewport journal; nothing stamped")
		return
	}

	disagree := 0
	for pos, wrapped := range stamps {
		if pos < len(journal) && journal[pos].wrapped != wrapped {
			disagree++
			where := "viewport"
			if journal[pos].inHistory {
				where = "history"
			}
			oracleReport("%s row %d: hint said wrapped=%v, oracle says %v: %q",
				where, journal[pos].index, journal[pos].wrapped, wrapped, journal[pos].text)
		}
	}
	oracleReport("%d/%d repaint rows aligned with history+viewport; %d safe boundaries, %d where hint and oracle disagree",
		len(alignment), len(narrowRows), len(stamps), disagree)

	if o.mode == winReflowOracle {
		applied, stale := display.applyReflowJournalFlags(journal, stamps)
		oracleReport("%d history+viewport boundaries stamped, %d became stale", applied, stale)
	}
}

func reflowRowPairKey(a, b string) string { return a + "\x00" + b }

type reflowRowAlignment struct {
	source int // row in the ConPTY narrow repaint
	target int // row in f4's history+viewport journal
}

// alignReflowRows computes an exact-row LCS. Empty rows are deliberately not
// anchors: a screenful of blanks has many equally valid alignments. The caller
// only trusts consecutive pairs from the result, turning the LCS into a set of
// verified row boundaries rather than a fuzzy whole-screen guess.
func alignReflowRows(source []string, target []reflowJournalRow) []reflowRowAlignment {
	dp := make([][]int, len(source)+1)
	for i := range dp {
		dp[i] = make([]int, len(target)+1)
	}
	for i := len(source) - 1; i >= 0; i-- {
		for j := len(target) - 1; j >= 0; j-- {
			if source[i] != "" && source[i] == target[j].text {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var out []reflowRowAlignment
	for i, j := 0, 0; i < len(source) && j < len(target); {
		if source[i] != "" && source[i] == target[j].text && dp[i][j] == dp[i+1][j+1]+1 {
			out = append(out, reflowRowAlignment{source: i, target: j})
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			i++
		} else {
			j++
		}
	}
	return out
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

// winReflowLogLines is what a run writes about its own reflow settings.
//
// The mode name alone is not enough to read a field log by. A field report of
// "the history does not come back when I resize" was once taken at face value
// and investigated as a matcher bug; the log had been made in `probe` mode,
// which re-wraps nothing by design, and nothing in it said so. Establishing
// that cost a round trip with the tester. So the line names the two switches
// the mode actually sets, and a mode that will not re-join the scrollback says
// that in words, next to the way to turn it on.
func winReflowLogLines(mode winReflowMode) []string {
	hint := mode != winReflowOff
	rewrap := mode == winReflowOracle
	oracle := mode == winReflowOracle || mode == winReflowProbe
	// absorb_repaint is named here because it is a behaviour change of this
	// session, not a long-standing one, and a field run that compares modes
	// needs to know which of them had it. The same goes for the history bound:
	// it used to be a row count and is now a logical-line count, and a log
	// from before that change looks identical without this line.
	lines := []string{fmt.Sprintf(
		"REFLOW: F4_WIN_REFLOW=%s hint_wrap=%v rewrap_on_resize=%v oracle_passes=%v absorb_repaint=%v history_bound=%d logical lines (hard ceiling %d rows)",
		mode, hint, rewrap, oracle, rewrap, maxGridHistoryLines, maxGridHistoryRowsHard)}
	if !rewrap {
		lines = append(lines, "REFLOW: this mode does not re-wrap on resize, so the "+
			"scrollback will not be re-joined however the window is dragged "+
			"(F4_WIN_REFLOW=oracle does, and is the Windows default)")
	}
	return lines
}

// absorbWindow is how long after a resize a repaint frame is still taken to
// be the repaint for that resize. A drag delivers one every few milliseconds;
// this only has to outlive the gap between ResizePseudoConsole and its frame.
var absorbWindow = 250 * time.Millisecond

// absorbResizeRepaint arms the absorber: for absorbWindow after a resize, the
// next ConPTY repaint frame is parsed into a scratch view and dropped instead
// of being applied to the display.
//
// Why drop it. f4 and ConPTY disagree about what the viewport should hold
// after a resize, and f4 is the one with the evidence: ConPTY keeps no
// scrollback (P16), so its repaint carries only the rows that fit the new
// size and blanks for the rest, while f4 has the session in GridHistory and
// has just re-laid it out. Letting the frame land replaced recovered rows
// with ConPTY's shorter view -- measured in the field as history flashing and
// being overwritten, and as blank rows over an intact history
// (docs/TERMINAL_CONPTY_FINDINGS.md 6.8, 6.15, 6.16). Nothing is lost by
// dropping the frame: every row in it reached f4 once, as ordinary output.
//
// Why arm rather than divert. The first version of this diverted the whole
// stream for the window, and a startup prompt that happened to arrive inside
// it never reached the display (O13). A frame is recognisable -- it opens
// with ESC[?25l and closes with ESC[?25h, on every build measured (P7, P14)
// -- so only a chunk that opens a frame is taken, and only until the frame
// closes. Ordinary output arriving in the window goes to the display as it
// always did. Swallowing it is no longer a matter of timing; it cannot
// happen.
//
// Only in oracle mode, where ReflowOnResize gives f4's own layout ownership
// of the viewport. In every other mode ConPTY's repaint is what keeps the
// screen right, and it must land.
func (o *reflowOracle) absorbResizeRepaint() {
	if o == nil || o.mode != winReflowOracle {
		return
	}
	tv := o.pf.termView
	if tv == nil || tv.UseAltScreen {
		return
	}
	o.mu.Lock()
	o.absorbArmedUntil = time.Now().Add(absorbWindow)
	o.mu.Unlock()
}

// frameOpen and frameClose bracket a ConPTY repaint (P7).
var (
	frameOpen  = []byte("\x1b[?25l")
	frameClose = []byte("\x1b[?25h")
)

// route decides where one chunk from the PTY goes: the scratch parser of a
// running oracle pass, the absorber if this chunk opens (or continues) a
// resize repaint while the absorber is armed, or nil for the display.
func (o *reflowOracle) route(data []byte) *AnsiParser {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.lastByte = time.Now()
	if o.sink != nil {
		// An oracle pass owns the stream.
		return o.sink
	}
	if o.absorbing != nil {
		// Inside a frame being absorbed: take it through to the close.
		p := o.absorbing
		if bytes.Contains(data, frameClose) {
			o.absorbing = nil
			o.absorbedFrames++
		}
		return p
	}
	if time.Now().Before(o.absorbArmedUntil) && bytes.HasPrefix(data, frameOpen) {
		// The frame is dropped, so nothing needs to be laid out and the
		// scratch view can be any size: reading the display's size here
		// raced with Resize writing it (the display's mutex is not held on
		// this path, and must not be -- it is the read loop).
		scratch := NewTerminalView(absorbScratchCols, absorbScratchRows)
		p := NewAnsiParser(scratch, nil)
		if bytes.Contains(data, frameClose) {
			// Whole frame in one chunk, the common case. Parsed here and
			// now, on the read loop: data is the loop's reusable buffer,
			// and handing it to another goroutine raced with the next
			// Read overwriting it.
			o.absorbedFrames++
			o.absorbArmedUntil = time.Time{}
			p.Process(data)
			scratch.Close()
			return discardParser
		}
		o.absorbing = p
		o.absorbArmedUntil = time.Time{}
		return p
	}
	return nil
}

// absorbScratchCols/Rows size the throwaway view an absorbed frame is parsed
// into. Wide enough that no ordinary frame wraps in it -- a wrapped scratch
// would cost time, not correctness -- and nothing more.
const (
	absorbScratchCols = 512
	absorbScratchRows = 128
)

// discardParser marks a chunk the caller must drop without parsing: the frame
// was already parsed and closed inside route.
var discardParser = NewAnsiParser(NewTerminalView(1, 1), nil)

// absorbArmed reports whether a resize repaint would currently be absorbed.
func (o *reflowOracle) absorbArmed() bool {
	if o == nil {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return time.Now().Before(o.absorbArmedUntil) || o.absorbing != nil
}
