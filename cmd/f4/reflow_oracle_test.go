package main

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/unxed/vtui"
)

// fakeConPTY reproduces what tools/conptyprobe recorded on 10.0.19045
// (docs/TERMINAL_LEDGER.md P5–P7). It keeps the shell's output as logical
// lines and renders them the way ConPTY does:
//
//   - live output: a line wider than the console is delivered as full rows
//     with a hard CRLF between them and no ESC[K; a row that ends short of
//     the width gets ESC[K before its CRLF (P6);
//   - on a width change, one frame: ESC[?25l ESC[H, every row + ESC[K,
//     CRLF between rows, ESC[r;cH ESC[?25h, with wrapped lines rejoined at
//     the new width (P7);
//   - PSEUDOCONSOLE_RESIZE_QUIRK would change nothing (P5), so the fake has
//     no flag at all;
//   - no scrollback: lines that no longer fit the height fall off the top.
//
// It is a PtyBackend, so it can sit where the real one sits.
// conptyBehaviour is what one Windows build's ConPTY was measured to do. Two
// builds are known (docs/TERMINAL_LEDGER.md, "Two Windows builds have been
// probed end to end"); a third is one more value here.
type conptyBehaviour struct {
	name string
	// repaintBreaksWrappedLines: the resize repaint writes a wrapped line as
	// rows joined by hard CRLF (19045, P6) rather than whole and let the
	// terminal's autowrap place it (22000, P12).
	repaintBreaksWrappedLines bool
	// sizeReport: the resize repaint opens with ESC[8;rows;cols t (P14).
	// Measured on 22000; not present in the 19045 log.
	sizeReport bool
}

var (
	conpty19045  = conptyBehaviour{name: "19045", repaintBreaksWrappedLines: true, sizeReport: false}
	conpty22000  = conptyBehaviour{name: "22000", repaintBreaksWrappedLines: false, sizeReport: true}
	conptyBuilds = []conptyBehaviour{conpty19045, conpty22000}
)

type fakeConPTY struct {
	mu     sync.Mutex
	b      conptyBehaviour
	cols   int
	rows   int
	lines  []string // logical lines, oldest first
	out    chan []byte
	closed bool
	// resizes records every SetSize call, for tests that check the oracle
	// restored the geometry.
	resizes []int
	// repaints counts frames sent for SetSize calls, same-size ones
	// included (6.16).
	repaints int
}

func newFakeConPTY(cols, rows int) *fakeConPTY {
	return newFakeConPTYFor(conpty22000, cols, rows)
}

func newFakeConPTYFor(b conptyBehaviour, cols, rows int) *fakeConPTY {
	return &fakeConPTY{b: b, cols: cols, rows: rows, out: make(chan []byte, 64)}
}

// print is a program writing text; each element is one logical line, sent
// the way ConPTY streams it live (P6).
func (f *fakeConPTY) print(lines ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var sb strings.Builder
	// ConPTY positions absolutely before it writes (TERMINAL.md, "absolute
	// grid obsession"); without this the display's bottom-aligned start and
	// the fake's top-aligned model would disagree about row numbers.
	fmt.Fprintf(&sb, "\x1b[%d;1H", f.viewRowsLocked()+1)
	for _, line := range lines {
		f.lines = append(f.lines, line)
		rows := wrapAt(line, f.cols)
		for i, row := range rows {
			if i < len(rows)-1 {
				// A wrapped row is full to the width: no erase, hard CRLF.
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
	f.out <- []byte(sb.String())
}

// prompt prints cmd's prompt with f4's injected marks, text first as on
// 26200; the mark-before-text order of 19045 is a property of the session
// tests, not of the reflow, and is covered there.
func (f *fakeConPTY) prompt(text string) {
	f.mu.Lock()
	row := f.viewRowsLocked() + 1
	f.lines = append(f.lines, text)
	f.trimToHeightLocked()
	f.mu.Unlock()
	f.out <- []byte(fmt.Sprintf("\x1b[%d;1H", row) + "\x1b]133;A\x1b\\" + text + "\x1b]133;B\x1b\\")
}

func (f *fakeConPTY) trimToHeightLocked() {
	for f.viewRowsLocked() > f.rows && len(f.lines) > 0 {
		f.lines = f.lines[1:]
	}
}

func (f *fakeConPTY) viewRowsLocked() int {
	n := 0
	for _, line := range f.lines {
		n += len(wrapAt(line, f.cols))
	}
	return n
}

// SetSize is ResizePseudoConsole. Every call repaints the whole viewport
// (P7) -- a height-only change too (6.15), and a call with the size it
// already has (6.16), which is the one case that comes back *without* the
// size report. An earlier version of this fake sent nothing for a height-only
// change; the field runs that cost were the ones described in 6.15.
func (f *fakeConPTY) SetSize(cols, rows int) {
	f.mu.Lock()
	f.resizes = append(f.resizes, cols)
	same := cols == f.cols && rows == f.rows
	f.cols, f.rows = cols, rows
	f.trimToHeightLocked()
	frame := f.repaintLocked(!same)
	f.repaints++
	f.mu.Unlock()
	f.out <- []byte(frame)
}

func (f *fakeConPTY) snapshotRepaints() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.repaints
}

func (f *fakeConPTY) snapshotResizes() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.resizes...)
}

// repaintLocked is the frame ConPTY sends after a resize: cursor hidden, the
// size report when the build sends one and the size changed, home, every row
// of the viewport, cursor restored and shown (P7, P14).
//
// The one thing the two builds do differently is how a wrapped line is laid
// out. 19045 writes each row and a hard CRLF between them, so the frame looks
// like the live stream (P6). 22000 writes the logical line whole and lets the
// terminal's autowrap place it; only the last row ends in CRLF (P12). On
// either build a short row is followed by ESC[K and a full row is not, and a
// hard-broken line exactly the width arrives as a full row plus CRLF with no
// ESC[K -- the one-in-W ambiguity of the hint (P13).
func (f *fakeConPTY) repaintLocked(sizeChanged bool) string {
	var sb strings.Builder
	sb.WriteString("\x1b[?25l")
	if f.b.sizeReport && sizeChanged {
		fmt.Fprintf(&sb, "\x1b[8;%d;%dt", f.rows, f.cols)
	}
	sb.WriteString("\x1b[H")
	var rows []string
	written := 0
	for _, line := range f.lines {
		parts := wrapAt(line, f.cols)
		rows = append(rows, parts...)
		for i, row := range parts {
			last := i == len(parts)-1
			full := len([]rune(row)) == f.cols
			sb.WriteString(row)
			if !full {
				sb.WriteString("\x1b[K")
			}
			written++
			if written >= f.rows {
				break
			}
			if last || f.b.repaintBreaksWrappedLines {
				sb.WriteString("\r\n")
			}
		}
		if written >= f.rows {
			break
		}
	}
	for ; written < f.rows; written++ {
		sb.WriteString("\x1b[K")
		if written < f.rows-1 {
			sb.WriteString("\r\n")
		}
	}
	// Cursor after the last line's text.
	cy, cx := 0, 0
	if len(rows) > 0 {
		cy = len(rows) - 1
		cx = len([]rune(rows[cy]))
	}
	fmt.Fprintf(&sb, "\x1b[%d;%dH\x1b[?25h", cy+1, cx+1)
	return sb.String()
}

func (f *fakeConPTY) Read(b []byte) (int, error) {
	data, ok := <-f.out
	if !ok {
		return 0, io.EOF
	}
	return copy(b, data), nil
}

func (f *fakeConPTY) Write(b []byte) (int, error) { return len(b), nil }
func (f *fakeConPTY) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.out)
	}
	return nil
}
func (f *fakeConPTY) Wait() error                           { return nil }
func (f *fakeConPTY) Run(name string, args ...string) error { return nil }
func (f *fakeConPTY) IsBusy() bool                          { return false }
func (f *fakeConPTY) ChildProcesses() []childProcess        { return nil }

func wrapAt(line string, cols int) []string {
	r := []rune(line)
	if len(r) == 0 {
		return []string{""}
	}
	var rows []string
	for len(r) > cols {
		rows = append(rows, string(r[:cols]))
		r = r[cols:]
	}
	return append(rows, string(r))
}

func padRight(s string, n int) string {
	for len([]rune(s)) < n {
		s += " "
	}
	return s
}

// reflowHarness is a PanelsFrame driven by a fakeConPTY, with a goroutine
// standing in for the local read loop.
type reflowHarness struct {
	t      *testing.T
	pf     *PanelsFrame
	pty    *fakeConPTY
	report []string
	repMu  sync.Mutex
}

func newReflowHarness(t *testing.T, mode winReflowMode, cols, rows int) *reflowHarness {
	t.Helper()
	oldTimeout, oldQuiet := oracleFrameTimeout, oracleQuietBefore
	oracleFrameTimeout, oracleQuietBefore = 200*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { oracleFrameTimeout, oracleQuietBefore = oldTimeout, oldQuiet })

	t.Cleanup(swapFrameManager(t))
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	pf := setupMockPanelsFrame(t)
	t.Cleanup(pf.Close)

	pty := newFakeConPTY(cols, rows)
	pf.pty = pty
	pf.termView.Resize(cols, rows)
	pf.parser = NewAnsiParser(pf.termView, nil)
	pf.setWinReflowMode(mode)

	h := &reflowHarness{t: t, pf: pf, pty: pty}
	oldReport := oracleReport
	oracleReport = func(format string, a ...any) {
		h.repMu.Lock()
		h.report = append(h.report, fmt.Sprintf(format, a...))
		h.repMu.Unlock()
	}
	t.Cleanup(func() { oracleReport = oldReport })

	go func() {
		buf := make([]byte, 65536)
		for {
			n, err := pty.Read(buf)
			if err != nil {
				return
			}
			pf.consumeLocalOutput(pty, buf[:n])
		}
	}()
	t.Cleanup(func() { pty.Close() })
	return h
}

func (h *reflowHarness) settle(d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		drainUITasks()
		time.Sleep(5 * time.Millisecond)
	}
}

func (h *reflowHarness) waitForOracle(timeout time.Duration) {
	h.t.Helper()
	// Waiting for the worker's lifecycle makes the assertions independent of
	// how long the race detector or a loaded runner takes to schedule it.
	deadline := time.Now().Add(timeout)
	started := false
	for time.Now().Before(deadline) {
		drainUITasks()
		h.pf.reflowOracle.mu.Lock()
		running := h.pf.reflowOracle.running
		h.pf.reflowOracle.mu.Unlock()
		resizes := h.pty.snapshotResizes()
		if running {
			started = true
		} else if len(resizes) > 0 {
			started = true
		}
		if started && !running {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatalf("reflow oracle did not finish: resizes=%v", h.pty.snapshotResizes())
}

func (h *reflowHarness) reported(substr string) bool {
	h.repMu.Lock()
	defer h.repMu.Unlock()
	for _, line := range h.report {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

// The scenario every mode is run through, as tools/conptyprobe recorded it:
// a banner, the echoed command line (78 characters: two rows at 40 columns),
// the command's output (60 characters: two rows), a short line, the prompt.
const (
	typedLine  = "C:\\2work-150>echo ABCDEFGHIJ0123456789abcdefghij0123456789ABCDEFGHIJ0123456789"
	outputLine = "ABCDEFGHIJ0123456789abcdefghij0123456789ABCDEFGHIJ0123456789"
)

// Rows at 40 columns: 0 banner, 1 empty, 2-3 typed line, 4-5 output, 6 short,
// 7 prompt. Wrapped: the first row of each two-row line.
var scenarioWrapped = map[int]bool{0: false, 1: false, 2: true, 3: false, 4: true, 5: false, 6: false, 7: false}

func TestWinReflowModeDefaultsToSafeOracleOnlyOnWindows(t *testing.T) {
	tests := []struct {
		value   string
		windows bool
		want    winReflowMode
	}{
		{"", true, winReflowOracle},
		{"", false, winReflowOff},
		{"off", true, winReflowOff},
		{"hint", true, winReflowHint},
		{"oracle", false, winReflowOracle},
		{"probe", true, winReflowProbe},
		{"unknown", true, winReflowOff},
	}
	for _, tt := range tests {
		if got := parseWinReflowMode(tt.value, tt.windows); got != tt.want {
			t.Errorf("parseWinReflowMode(%q, windows=%v) = %s, want %s",
				tt.value, tt.windows, got, tt.want)
		}
	}
}

func (h *reflowHarness) playScenario() {
	h.pty.print("Microsoft Windows [Version 10.0]", "", typedLine, outputLine, "short")
	h.pty.prompt("C:\\2work-150>")
	h.settle(60 * time.Millisecond)
}

func (h *reflowHarness) flags() []bool { return h.pf.termView.WrapFlagsCopy() }

// Without any mode, ConPTY's hard CRLFs are taken at face value: nothing is
// marked wrapped. This is the control group.
func TestWinReflowOffKeepsHardBreaks(t *testing.T) {
	h := newReflowHarness(t, winReflowOff, 40, 12)
	h.playScenario()
	for y, w := range h.flags() {
		if w {
			t.Errorf("row %d marked wrapped with the mode off", y)
		}
	}
	if got := h.pty.snapshotResizes(); len(got) != 0 {
		t.Errorf("mode off resized the pseudoconsole: %v", got)
	}
}

// The hint reads P6: the two full rows of the wide line with no ESC[K are
// wrapped, the short rows are not.
func TestWinReflowHintMarksFullRowsWithoutErase(t *testing.T) {
	h := newReflowHarness(t, winReflowHint, 40, 12)
	h.playScenario()
	flags := h.flags()
	for y, w := range scenarioWrapped {
		if flags[y] != w {
			t.Errorf("hint: row %d wrapped=%v, want %v", y, flags[y], w)
		}
	}
	if got := h.pty.snapshotResizes(); len(got) != 0 {
		t.Errorf("hint resized the pseudoconsole: %v", got)
	}
}

// The oracle asks ConPTY: resize wide, read the rejoined frame, resize back,
// and stamp the viewport rows. The display must not change at all, the
// pseudoconsole must be back at its real width, and the stamps must match
// the truth the fake was built from.
func TestWinReflowOracleStampsFromConPTYsOwnReflow(t *testing.T) {
	h := newReflowHarness(t, winReflowOracle, 40, 12)
	h.playScenario()
	rowsBefore := h.pf.termView.RowTexts()
	cursorBefore := h.pf.termView.PromptSnapshot()

	// The session's settled prompt is what triggers a pass; drive it as the
	// session would.
	h.pf.runReflowOracle()
	h.waitForOracle(2 * time.Second)

	if got := h.pty.snapshotResizes(); len(got) != 2 || got[0] != oracleWideColumns || got[1] != 40 {
		t.Fatalf("oracle resizes = %v, want [%d 40]", got, oracleWideColumns)
	}
	if rows := h.pf.termView.RowTexts(); strings.Join(rows, "\n") != strings.Join(rowsBefore, "\n") {
		t.Errorf("the display changed during the pass:\n%q\nwas\n%q", rows, rowsBefore)
	}
	if after := h.pf.termView.PromptSnapshot(); after != cursorBefore {
		t.Errorf("cursor moved during the pass: %+v -> %+v", cursorBefore, after)
	}
	flags := h.flags()
	for y, w := range scenarioWrapped {
		if flags[y] != w {
			t.Errorf("oracle: row %d wrapped=%v, want %v", y, flags[y], w)
		}
	}
	if !h.reported("delimited=true") {
		t.Errorf("frames were not recognised as delimited by ?25h; report: %v", h.report)
	}
}

// Probe mode runs the oracle and logs what it finds, but writes nothing:
// the hint's stamps stay, and the report says where the two disagree.
func TestWinReflowProbeLogsButDoesNotStamp(t *testing.T) {
	h := newReflowHarness(t, winReflowProbe, 40, 12)
	h.playScenario()
	before := h.flags()
	h.pf.runReflowOracle()
	h.waitForOracle(2 * time.Second)

	if after := h.flags(); strings.Join(boolsToStrings(after), "") != strings.Join(boolsToStrings(before), "") {
		t.Errorf("probe changed the flags: %v -> %v", before, after)
	}
	if !h.reported("safe boundaries") {
		t.Errorf("probe produced no comparison report: %v", h.report)
	}
	if got := h.pty.snapshotResizes(); len(got) != 2 {
		t.Errorf("probe resizes = %v, want two", got)
	}
}

// A row which differs between the repaint and the journal breaks the exact
// run around it. Boundaries elsewhere may still be stamped, but the boundary
// touching the changed row must retain its old value.
func TestWinReflowOracleSkipsBoundaryAtMismatch(t *testing.T) {
	h := newReflowHarness(t, winReflowOracle, 40, 12)
	h.playScenario()
	// Start from deliberately wrong flags, then corrupt row 5. Row 2 belongs
	// to an earlier exact run and can be corrected; row 4 continues into the
	// corrupted row 5 and must not be guessed.
	h.pf.termView.mu.Lock()
	for i := range h.pf.termView.WrapFlags {
		h.pf.termView.WrapFlags[i] = false
	}
	h.pf.termView.Lines[5][0].Char = 'X'
	h.pf.termView.mu.Unlock()
	h.pf.runReflowOracle()
	h.waitForOracle(2 * time.Second)
	flags := h.flags()
	if !flags[2] {
		t.Fatalf("safe run before the mismatch was not stamped: %v; report=%v", flags, h.report)
	}
	if flags[4] {
		t.Errorf("boundary touching a mismatched row was stamped: %v; report=%v", flags, h.report)
	}
}

// The real 22000 log exposed the important offset: ConPTY repainted its
// viewport from the Windows banner while f4 had already moved several of
// those rows into GridHistory. The oracle must find the repaint in the
// combined journal and stamp rows which are no longer on f4's screen.
func TestWinReflowOracleStampsRowsAlreadyInLocalHistory(t *testing.T) {
	h := newReflowHarness(t, winReflowOracle, 40, 12)
	h.playScenario()
	h.pf.termView.mu.Lock()
	for i := range h.pf.termView.WrapFlags {
		h.pf.termView.WrapFlags[i] = false
	}
	h.pf.termView.mu.Unlock()

	// Scroll five rows only in the local model. The fake ConPTY deliberately
	// retains its viewport, reproducing the vertical disagreement from the
	// field log while leaving the lost rows in f4's journal.
	h.pf.termView.SetCursor(0, h.pf.termView.Height-1)
	for range 5 {
		h.pf.termView.Index()
	}
	if len(h.pf.termView.GridHistoryWrap) < 5 {
		t.Fatalf("setup did not create history: %d rows", len(h.pf.termView.GridHistoryWrap))
	}

	h.pf.runReflowOracle()
	h.waitForOracle(2 * time.Second)
	wantWrapped := map[string]bool{
		"C:\\2work-150>echo ABCDEFGHIJ0123456789ab": false,
		"ABCDEFGHIJ0123456789abcdefghij0123456789":  false,
	}
	for i, row := range h.pf.termView.GridHistory {
		if _, wanted := wantWrapped[rowText(row)]; wanted && h.pf.termView.GridHistoryWrap[i] {
			wantWrapped[rowText(row)] = true
		}
	}
	for row, found := range wantWrapped {
		if !found {
			t.Fatalf("oracle did not stamp evicted wrapped row %q in history: %v; report=%v",
				row, h.pf.termView.GridHistoryWrap, h.report)
		}
	}
	if !h.reported("history row") || !h.reported("history+viewport boundaries stamped") {
		t.Fatalf("history stamping was not reported: %v", h.report)
	}
}

// On Windows the oracle enables local reflow for this view even though the
// package-wide fallback remains disabled there. ConPTY will repaint the live
// viewport, while this local pass is what brings confirmed history along.
func TestWinReflowOracleModeReflowsLocalHistory(t *testing.T) {
	old := terminalReflowEnabled
	terminalReflowEnabled = false
	t.Cleanup(func() { terminalReflowEnabled = old })

	tv := NewTerminalView(4, 2)
	defer tv.Close()
	tv.ReflowOnResize = true
	tv.SetCursor(0, 0)
	for _, r := range "abcdefghij" {
		tv.PutChar(r, DefaultTermAttr)
	}
	if len(tv.GridHistory) == 0 || !tv.GridHistoryWrap[0] {
		t.Fatalf("setup did not retain a wrapped history row: history=%d flags=%v",
			len(tv.GridHistory), tv.GridHistoryWrap)
	}

	tv.Resize(12, 2)
	var joined strings.Builder
	for _, hist := range tv.GridHistory {
		joined.WriteString(rowText(hist))
	}
	for _, row := range tv.Lines {
		joined.WriteString(rowText(row))
	}
	if got := joined.String(); got != "abcdefghij" {
		t.Fatalf("oracle-owned resize lost/repeated local history: %q", got)
	}
}

// The editable journal is bounded, so a resize can afford to reflow all of
// it. This guards against the old 256-row cutoff leaving most scrollback at
// the previous width.
func TestWinReflowOracleModeReflowsEntireLocalJournal(t *testing.T) {
	old := terminalReflowEnabled
	terminalReflowEnabled = false
	t.Cleanup(func() { terminalReflowEnabled = old })

	tv := NewTerminalView(4, 2)
	defer tv.Close()
	tv.ReflowOnResize = true
	tv.SetCursor(0, 0)
	for range 300 {
		for _, r := range "abcdefgh" {
			tv.PutChar(r, DefaultTermAttr)
		}
		tv.PutChar('\r', DefaultTermAttr)
		tv.PutChar('\n', DefaultTermAttr)
	}
	if len(tv.GridHistory) <= 256 {
		t.Fatalf("setup has only %d history rows, want more than the old cutoff", len(tv.GridHistory))
	}

	tv.Resize(10, 2)
	for i, row := range tv.GridHistory {
		if len(row) != 10 {
			t.Fatalf("history row %d kept old width %d after full-journal reflow", i, len(row))
		}
	}
}

// The matcher on its own, against the exact rows of the probe log.
func TestMatchWrappedRows(t *testing.T) {
	wide := []string{
		"Microsoft Windows [Version 10.0]",
		"",
		typedLine,
		outputLine,
		"short",
		"C:\\2work-150>",
	}
	// The narrow rows exactly as the probe log shows them at 40 columns,
	// trailing padding already trimmed.
	narrow := []string{
		"Microsoft Windows [Version 10.0]",
		"",
		"C:\\2work-150>echo ABCDEFGHIJ0123456789ab",
		"cdefghij0123456789ABCDEFGHIJ0123456789",
		"ABCDEFGHIJ0123456789abcdefghij0123456789",
		"ABCDEFGHIJ0123456789",
		"short",
		"C:\\2work-150>",
		"", "", "", "",
	}
	flags, ok := matchWrappedRows(wide, narrow)
	if !ok {
		t.Fatal("rows from the probe did not match")
	}
	want := scenarioWrapped
	for y, w := range want {
		if flags[y] != w {
			t.Errorf("row %d wrapped=%v, want %v", y, flags[y], w)
		}
	}
	if _, ok := matchWrappedRows([]string{"abc"}, []string{"abd"}); ok {
		t.Error("different text was matched")
	}
	// A real space at the seam of a wrapped row, padded by ConPTY.
	flags, ok = matchWrappedRows([]string{"abc def"}, []string{"abc", "def"})
	if !ok || !flags[0] || flags[1] {
		t.Errorf("space at the seam: ok=%v flags=%v", ok, flags)
	}
}

func boolsToStrings(b []bool) []string {
	out := make([]string, len(b))
	for i, v := range b {
		if v {
			out[i] = "1"
		} else {
			out[i] = "0"
		}
	}
	return out
}

// The oracle is not a thing tests call: the cmd session calls it when a
// prompt settles with no console child. Drive the session's own path and
// confirm the pass happened.
func TestWinReflowOracleFiresFromSettledPrompt(t *testing.T) {
	h := newReflowHarness(t, winReflowOracle, 40, 12)
	oldSettle, oldRecheck := cmdPromptSettleDelay, cmdPromptRecheckDelay
	cmdPromptSettleDelay, cmdPromptRecheckDelay = 20*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { cmdPromptSettleDelay, cmdPromptRecheckDelay = oldSettle, oldRecheck })
	h.pf.cmdSession = newCmdShellSession(h.pf)
	h.pf.termView.OnShellMark = func(mark string, snap promptSnapshot) { h.pf.cmdSession.handleMark(mark, snap) }

	h.playScenario() // ends with a prompt whose B mark the session sees
	h.waitForOracle(2 * time.Second)

	if got := h.pty.snapshotResizes(); len(got) != 2 {
		t.Fatalf("a settled prompt did not trigger an oracle pass; resizes=%v report=%v", got, h.report)
	}
	flags := h.flags()
	for y, w := range scenarioWrapped {
		if flags[y] != w {
			t.Errorf("row %d wrapped=%v, want %v", y, flags[y], w)
		}
	}
}

// Nothing here touches Unix: with the hint off (the default outside
// Windows), a full row followed by CRLF is a hard break as it always was.
func TestHintOffLeavesUnixWrapsAlone(t *testing.T) {
	// The view starts with its cursor on the bottom row (bottom-aligned
	// initialisation); pin it to the top so row indices are what they read.
	tv := NewTerminalView(10, 4)
	defer tv.Close()
	tv.SetCursor(0, 0)
	p := NewAnsiParser(tv, nil)
	p.Process([]byte("0123456789\r\nnext"))
	if tv.WrapFlagsCopy()[0] {
		t.Error("a full row ending in CRLF was marked wrapped with the hint off")
	}
	// And a genuine soft wrap (no CRLF, text runs past the edge) still is.
	tv2 := NewTerminalView(10, 4)
	defer tv2.Close()
	tv2.SetCursor(0, 0)
	NewAnsiParser(tv2, nil).Process([]byte("0123456789abc"))
	if !tv2.WrapFlagsCopy()[0] {
		t.Error("a genuine soft wrap was lost")
	}
}

func TestWinReflowLogLinesNameTheSwitchesNotJustTheMode(t *testing.T) {
	// A field log must answer "was re-wrapping even on?" without anyone
	// reading panels_frame.go to find out.
	oracle := winReflowLogLines(winReflowOracle)
	if len(oracle) != 1 {
		t.Fatalf("oracle mode should not warn: %q", oracle)
	}
	if !strings.Contains(oracle[0], "absorb_repaint=true") ||
		!strings.Contains(oracle[0], "history_bound=") ||
		!strings.Contains(oracle[0], "rewrap_on_resize=true") ||
		!strings.Contains(oracle[0], "hint_wrap=true") ||
		!strings.Contains(oracle[0], "oracle_passes=true") {
		t.Fatalf("oracle line does not name its switches: %q", oracle[0])
	}

	for _, mode := range []winReflowMode{winReflowOff, winReflowHint, winReflowProbe} {
		lines := winReflowLogLines(mode)
		if !strings.Contains(lines[0], "rewrap_on_resize=false") {
			t.Errorf("%v: expected rewrap off, got %q", mode, lines[0])
		}
		if len(lines) != 2 || !strings.Contains(lines[1], "F4_WIN_REFLOW=oracle") {
			t.Errorf("%v: a mode that does not re-wrap must say so and name the mode that does: %q", mode, lines)
		}
	}

	// The probe's matrix greps for this prefix; keep it parseable.
	if !strings.HasPrefix(winReflowLogLines(winReflowProbe)[0], "REFLOW: F4_WIN_REFLOW=probe") {
		t.Fatal("tools/conptyprobe matches on this prefix")
	}
}

func TestAbsorbResizeRepaintOnlyInOracleMode(t *testing.T) {
	frame := []byte("\x1b[?25l\x1b[8;24;80t\x1b[Hrow from ConPTY\x1b[K\x1b[?25h")
	for _, mode := range []winReflowMode{winReflowOff, winReflowHint, winReflowProbe} {
		pf := &PanelsFrame{termView: NewTerminalView(80, 24)}
		o := newReflowOracle(pf, mode)
		o.absorbResizeRepaint()
		if o.route(frame) != nil {
			t.Errorf("%v: the repaint must reach the display in this mode", mode)
		}
		pf.termView.Close()
	}
}

// The corner-drag bug of 6.15/6.16: after any resize the next repaint frame
// must not reach the display, whatever the resize changed.
func TestAbsorbResizeRepaintKeepsTheFrameOffTheDisplay(t *testing.T) {
	pf := &PanelsFrame{termView: NewTerminalView(80, 24)}
	defer pf.termView.Close()
	o := newReflowOracle(pf, winReflowOracle)

	o.absorbResizeRepaint()
	frame := []byte("\x1b[?25l\x1b[8;24;80t\x1b[Hrow from ConPTY\x1b[K\x1b[?25h")
	sink := o.route(frame)
	if sink == nil {
		t.Fatal("an armed absorber must take a repaint frame")
	}
	if sink != discardParser {
		sink.Process(frame)
	}
	if got := pf.termView.RowTexts()[0]; strings.Contains(got, "ConPTY") {
		t.Fatalf("the frame reached the display: %q", got)
	}
	// One frame per resize: the next chunk, frame or not, is for the display.
	if o.route(frame) != nil {
		t.Fatal("a second frame after one resize must reach the display")
	}
}

// A frame with no size report -- what a same-size ResizePseudoConsole
// produces (6.16) -- is still a frame and is still absorbed.
func TestAbsorbTakesASizelessRepaint(t *testing.T) {
	pf := &PanelsFrame{termView: NewTerminalView(80, 24)}
	defer pf.termView.Close()
	o := newReflowOracle(pf, winReflowOracle)
	o.absorbResizeRepaint()
	if o.route([]byte("\x1b[?25l\x1bHthree rows\x1b[K\r\n\x1b[K\x1b[?25h")) == nil {
		t.Fatal("a same-size repaint must be absorbed like any other")
	}
}

// O13: ordinary output arriving inside the absorb window must reach the
// display. The first absorber diverted the whole stream for 250ms and lost a
// startup prompt that way.
func TestAbsorbNeverTakesOrdinaryOutput(t *testing.T) {
	pf := &PanelsFrame{termView: NewTerminalView(80, 24)}
	defer pf.termView.Close()
	o := newReflowOracle(pf, winReflowOracle)
	o.absorbResizeRepaint()
	for _, chunk := range []string{"C:\\>", "\x1b]133;A\x1b\\prompt\x1b]133;B\x1b\\", "dir output\r\n", "\x1b[2Jcleared"} {
		if o.route([]byte(chunk)) != nil {
			t.Fatalf("ordinary output %q was taken by the absorber", chunk)
		}
	}
	// And the arming survives that output, so the frame that follows is
	// still recognised.
	if o.route([]byte("\x1b[?25l\x1b[H\x1b[K\x1b[?25h")) == nil {
		t.Fatal("the frame after ordinary output must still be absorbed")
	}
}

// A frame split across two reads is taken to its close and no further.
func TestAbsorbFollowsASplitFrameToItsClose(t *testing.T) {
	pf := &PanelsFrame{termView: NewTerminalView(80, 24)}
	defer pf.termView.Close()
	o := newReflowOracle(pf, winReflowOracle)
	o.absorbResizeRepaint()
	if o.route([]byte("\x1b[?25l\x1b[8;24;80t\x1b[Hfirst half")) == nil {
		t.Fatal("the opening chunk must be taken")
	}
	if o.route([]byte("second half\x1b[K\x1b[?25h")) == nil {
		t.Fatal("the closing chunk must be taken")
	}
	if o.route([]byte("after the frame")) != nil {
		t.Fatal("output after the close must reach the display")
	}
}

func TestAbsorbNeverStealsFromAPass(t *testing.T) {
	pf := &PanelsFrame{termView: NewTerminalView(80, 24)}
	defer pf.termView.Close()
	o := newReflowOracle(pf, winReflowOracle)
	pass := NewAnsiParser(NewTerminalView(80, 24), nil)
	o.mu.Lock()
	o.running, o.sink = true, pass
	o.mu.Unlock()
	o.absorbResizeRepaint()
	if got := o.route([]byte("\x1b[?25l\x1b[H\x1b[?25h")); got != pass {
		t.Fatal("a running oracle pass must keep the stream")
	}
}

// The absorber expires: a frame long after the resize is ordinary output.
func TestAbsorbExpires(t *testing.T) {
	old := absorbWindow
	absorbWindow = 10 * time.Millisecond
	t.Cleanup(func() { absorbWindow = old })
	pf := &PanelsFrame{termView: NewTerminalView(80, 24)}
	defer pf.termView.Close()
	o := newReflowOracle(pf, winReflowOracle)
	o.absorbResizeRepaint()
	time.Sleep(30 * time.Millisecond)
	if o.route([]byte("\x1b[?25l\x1b[H\x1b[?25h")) != nil {
		t.Fatal("a frame after the window must reach the display")
	}
}
