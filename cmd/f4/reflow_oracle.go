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

// winReflowModeFromEnv picks the Windows reflow mode: F4_WIN_REFLOW in the
// environment first, then [Terminal] WindowsReflow in the config, then the
// oracle, which is the Windows default. Every stamp the oracle makes is
// checked against both ConPTY frames and the local journal before it can
// change history.
//
// The config key is the conservative switch. Everything the reflow rests on
// was measured on two builds, 19045 and 22000 (docs/CONPTY_RESEARCH.md); on
// a build where ConPTY behaves differently, "off" returns the terminal to
// Horizontal Preservation, which asks nothing of ConPTY beyond what every
// build has done since 1809. The environment variable stays above it so a
// field run can override a config without editing it.
func winReflowModeFromEnv() winReflowMode {
	return winReflowModeFrom(os.Getenv("F4_WIN_REFLOW"), AppConfig.WindowsReflow, runtime.GOOS == "windows")
}

func winReflowModeFrom(env, config string, windows bool) winReflowMode {
	if strings.TrimSpace(env) != "" {
		return parseWinReflowMode(env, windows)
	}
	if !windows {
		// The key is Windows-only by name and by meaning; the environment
		// variable above can still force a mode on for tests elsewhere.
		return winReflowOff
	}
	switch strings.ToLower(strings.TrimSpace(config)) {
	case "", "auto", "default":
		return parseWinReflowMode("", windows)
	default:
		return parseWinReflowMode(config, windows)
	}
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

// oracleReportMu guards oracleReport. In production it is never reassigned;
// the tests swap it to capture lines, and the absorber now reports from the
// read loop, so the swap and the call must not race.
var oracleReportMu sync.RWMutex

func report(format string, a ...any) {
	oracleReportMu.RLock()
	f := oracleReport
	oracleReportMu.RUnlock()
	f(format, a...)
}

// setOracleReport swaps the reporter for a test and returns the restore.
func setOracleReport(f func(string, ...any)) func() {
	oracleReportMu.Lock()
	old := oracleReport
	oracleReport = f
	oracleReportMu.Unlock()
	return func() {
		oracleReportMu.Lock()
		oracleReport = old
		oracleReportMu.Unlock()
	}
}

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
	// absorbing is the scratch parser of a resize repaint being taken
	// across reads; resizesSeen and absorbedFrames are for the log.
	absorbing      *AnsiParser
	absorbedFrames int
	resizesSeen    int
	// passesRun counts oracle passes started, for the session summary.
	passesRun int
	// pendingRepaints is how many resize repaints ConPTY still owes us.
	pendingRepaints int
	// absorbedBytes counts the open repaint's size, for the give-up guard.
	absorbedBytes int
	lastByte      time.Time
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
	o.passesRun++
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
		report("display changed during the pass (%+v -> %+v); nothing stamped", before, after)
		return
	}

	wideRows := trimTrailingEmpty(wide.view.RowTexts())
	narrowRows := narrow.view.RowTexts()
	journal := display.reflowJournalSnapshot()

	report("wide frame: %d content rows, delimited=%v; narrow frame: delimited=%v; viewport %dx%d",
		len(wideRows), wide.delimited, narrow.delimited, cols, rows)
	if len(wideRows) > rows {
		report("wide frame holds more rows than the viewport: conhost keeps scrollback under ConPTY")
	}

	flags, ok := matchWrappedRows(wideRows, narrowRows)
	if !ok {
		report("mismatch: the wide and narrow frames do not describe the same text; nothing stamped")
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
		report("mismatch: no consecutive repaint rows occur in the local history+viewport journal; nothing stamped")
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
			report("%s row %d: hint said wrapped=%v, oracle says %v: %q",
				where, journal[pos].index, journal[pos].wrapped, wrapped, journal[pos].text)
		}
	}
	report("%d/%d repaint rows aligned with history+viewport; %d safe boundaries, %d where hint and oracle disagree",
		len(alignment), len(narrowRows), len(stamps), disagree)

	if o.mode == winReflowOracle {
		applied, stale := display.applyReflowJournalFlags(journal, stamps)
		report("%d history+viewport boundaries stamped, %d became stale", applied, stale)
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

// absorbResizeRepaint used to arm a time window after a resize. It is kept
// as the call the resize path makes, and it now records only that a resize
// happened; what identifies a resize repaint is the frame itself.
//
// Why no window and no cursor-hide heuristic. ConPTY hides the cursor
// (ESC[?25l ... ESC[?25h) around *every* batch it writes, not only around a
// resize repaint. While a command is printing, every chunk opens that way.
// An absorber that took "a frame within 250ms of a resize" therefore took
// real output whenever the user resized during a `dir`, and the Terminal
// Log lost the middle of its lines -- the one failure this whole design must
// never produce (docs/CONPTY_RESEARCH.md 6.18).
//
// What a resize repaint has that ordinary output never has is the XTWINOPS
// size report, ESC[8;rows;cols t, immediately after the cursor hide (P14).
// So that report is the criterion, and the only one: a frame that carries it
// is ConPTY laying out its screen for a size f4 has already laid out itself,
// and is dropped whenever it arrives -- late, split across reads, or after a
// resize this instance did not initiate. A frame without it is output, and
// goes to the display. Nothing is lost by dropping the frame: every row in it
// reached f4 once, as output, before it scrolled (P16).
//
// On a build whose repaints carry no size report the absorber never fires,
// the repaint lands after f4's re-wrap and overwrites it, and the screen is
// wrong until the next resize -- visible, and recoverable, and preferable to
// guessing. That build is the portability question of the ledger (O4).
//
// Only in oracle mode, where ReflowOnResize gives f4's own layout ownership
// of the viewport. In every other mode ConPTY's repaint must land.
func (o *reflowOracle) absorbResizeRepaint() {
	if o == nil || o.mode != winReflowOracle {
		return
	}
	o.mu.Lock()
	o.resizesSeen++
	// One repaint is now owed. ConPTY sends exactly one per
	// ResizePseudoConsole call, and this is called only when that call is
	// actually made, so the count is what says a home-repaint is ConPTY
	// answering f4 rather than a program redrawing its own screen. It is not
	// a time window: a repaint that trails its resize by a second still
	// arrives owed. The clamp bounds the damage if a build ever sends no
	// repaint for a resize -- at worst one later frame is misread, once.
	if o.pendingRepaints < maxPendingRepaints {
		o.pendingRepaints++
	}
	o.mu.Unlock()
}

// reportAbsorbLocked writes one line per absorbed repaint, and only for the
// first few of a burst plus a running total after that. A drag absorbs
// hundreds; a log that carries every one buries the lines that matter, and
// the reader of these logs is usually a model with a budget.
func (o *reflowOracle) reportAbsorbLocked(n int, whole bool) {
	if o.absorbedFrames <= 3 || o.absorbedFrames%50 == 0 {
		report("absorbed resize repaint #%d (%d bytes, whole=%v); %d resizes seen, %d repaints still owed",
			o.absorbedFrames, n, whole, o.resizesSeen, o.pendingRepaints)
	}
}

// maxPendingRepaints bounds the owed-repaint count; a drag can outrun the
// stream briefly, but nothing legitimate owes more than a few.
const maxPendingRepaints = 64

// frameOpen and frameClose bracket every ConPTY write batch (P7); sizeReport
// is what only a resize repaint carries (P14).
var (
	frameOpen  = []byte("\x1b[?25l")
	frameClose = []byte("\x1b[?25h")
	sizeReport = []byte("\x1b[8;")
)

// routeStep is one decision about the front of a chunk: send the first n
// bytes to sink (nil sink means the display), then ask again about the rest.
//
// A read from ConPTY is not a message. One read can carry the tail of a
// command batch, a whole resize repaint and the head of the next batch, so
// the absorber must take exactly the repaint and hand everything around it
// on. Taking a chunk whole ate the output on either side of a coalesced
// frame in review (docs/TERMINAL_CONPTY_FINDINGS.md 6.21).
type routeStep struct {
	sink *AnsiParser
	n    int
}

// route decides where the front of one chunk from the PTY goes: the scratch
// parser of a running oracle pass, the absorber for a resize repaint that
// ConPTY owes, or the display. It never decides about more than the bytes
// it can classify, so consumeLocalOutput calls it in a loop.
func (o *reflowOracle) route(data []byte) routeStep {
	if o == nil || len(data) == 0 {
		return routeStep{nil, len(data)}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.lastByte = time.Now()
	if o.sink != nil {
		// An oracle pass owns the stream, all of it.
		return routeStep{o.sink, len(data)}
	}
	if o.absorbing != nil {
		// Inside a repaint being absorbed: take it through to the close and
		// not one byte further. And not forever: no measured build omits
		// the close, which is exactly why this is guarded -- the cost of
		// being wrong would be every byte after it.
		p := o.absorbing
		if i := bytes.Index(data, frameClose); i >= 0 {
			n := i + len(frameClose)
			o.absorbing = nil
			o.absorbedFrames++
			o.absorbedBytes = 0
			o.reportAbsorbLocked(n, false)
			return routeStep{p, n}
		}
		o.absorbedBytes += len(data)
		if o.absorbedBytes > maxAbsorbBytes {
			o.absorbing = nil
			o.absorbedBytes = 0
			report("gave up on a resize repaint with no close after %d bytes; the stream goes to the display", maxAbsorbBytes)
			return routeStep{nil, len(data)}
		}
		return routeStep{p, len(data)}
	}
	if o.mode != winReflowOracle || o.pendingRepaints == 0 {
		return routeStep{nil, len(data)}
	}
	start := resizeRepaintStart(data)
	if start < 0 {
		return routeStep{nil, len(data)}
	}
	if start > 0 {
		// Output before the frame belongs to the display; decide about the
		// frame on the next call.
		return routeStep{nil, start}
	}
	// Never on the alternate screen. A full-screen program -- including f4
	// itself running inside f4's terminal -- switches to it and repaints
	// from home exactly like a resize repaint. There f4 does not re-wrap at
	// all, so ConPTY's repaint is the only thing keeping the screen right
	// and must land.
	if tv := o.pf.termView; tv == nil || tv.OnAltScreen() {
		return routeStep{nil, len(data)}
	}
	o.pendingRepaints--
	// The frame is dropped, so nothing needs to be laid out and the
	// scratch view can be any size: reading the display's size here raced
	// with Resize writing it (the display's mutex is not held on this path,
	// and must not be -- it is the read loop).
	scratch := NewTerminalView(absorbScratchCols, absorbScratchRows)
	p := NewAnsiParser(scratch, nil)
	if i := bytes.Index(data, frameClose); i >= 0 {
		// Whole frame in this chunk, the common case: parsed here and now
		// on the read loop (data is the loop's reusable buffer, and handing
		// it to another goroutine raced with the next Read), and only up to
		// the close. Whatever follows is the next batch.
		n := i + len(frameClose)
		o.absorbedFrames++
		o.reportAbsorbLocked(n, true)
		p.Process(data[:n])
		scratch.Close()
		return routeStep{discardParser, n}
	}
	o.absorbing = p
	o.absorbedBytes = len(data)
	return routeStep{p, len(data)}
}

// maxAbsorbBytes bounds one absorbed repaint. A full screen at the largest
// plausible size is a few hundred kilobytes of cells and escapes; nothing
// legitimate is larger, and past it the only safe reading is "this was not a
// frame after all".
var maxAbsorbBytes = 1 << 20

// resizeRepaintStart returns where a resize repaint begins in data, or -1.
//
// A repaint of the whole viewport and a batch of command output both open
// with the cursor hide (ESC[?25l), so the hide alone is not it -- taking a
// chunk on the hide is what ate the Terminal Log on the photo (6.18). What
// a resize repaint has that a command batch does not: it positions the
// cursor at **home** right after the hide (and after the size report on
// builds that send one, P14), because it is redrawing the screen from the
// top. A command batch positions at the row it is about to write, below
// home, since it is appending under what is already there.
func resizeRepaintStart(data []byte) int {
	from := 0
	for {
		i := bytes.Index(data[from:], frameOpen)
		if i < 0 {
			return -1
		}
		i += from
		rest := data[i+len(frameOpen):]
		if bytes.HasPrefix(rest, sizeReport) {
			if j := bytes.IndexByte(rest, 't'); j >= 0 {
				rest = rest[j+1:]
			}
		}
		if bytes.HasPrefix(rest, []byte("\x1b[H")) || bytes.HasPrefix(rest, []byte("\x1b[1;1H")) {
			return i
		}
		from = i + len(frameOpen)
	}
}

// isResizeRepaint is resizeRepaintStart for the tests that ask yes or no.
func isResizeRepaint(data []byte) bool { return resizeRepaintStart(data) == 0 }

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

// absorbArmed reports whether a resize repaint is currently being absorbed.
func (o *reflowOracle) absorbArmed() bool {
	if o == nil {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.absorbing != nil
}
