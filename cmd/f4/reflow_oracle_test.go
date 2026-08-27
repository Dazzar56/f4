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
type fakeConPTY struct {
	mu     sync.Mutex
	cols   int
	rows   int
	lines  []string // logical lines, oldest first
	out    chan []byte
	closed bool
	// resizes records every SetSize call, for tests that check the oracle
	// restored the geometry.
	resizes []int
}

func newFakeConPTY(cols, rows int) *fakeConPTY {
	return &fakeConPTY{cols: cols, rows: rows, out: make(chan []byte, 64)}
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

// SetSize is ResizePseudoConsole. A width change repaints (P7); a
// height-only change does not, which is also what the probe showed.
func (f *fakeConPTY) SetSize(cols, rows int) {
	f.mu.Lock()
	f.resizes = append(f.resizes, cols)
	if cols == f.cols {
		f.rows = rows
		f.mu.Unlock()
		return
	}
	f.cols, f.rows = cols, rows
	f.trimToHeightLocked()
	frame := f.repaintLocked()
	f.mu.Unlock()
	f.out <- []byte(frame)
}

func (f *fakeConPTY) snapshotResizes() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.resizes...)
}

func (f *fakeConPTY) repaintLocked() string {
	var rows []string
	for _, line := range f.lines {
		rows = append(rows, wrapAt(line, f.cols)...)
	}
	var sb strings.Builder
	sb.WriteString("\x1b[?25l\x1b[H")
	for y := 0; y < f.rows; y++ {
		if y < len(rows) {
			sb.WriteString(rows[y])
		}
		sb.WriteString("\x1b[K")
		if y < f.rows-1 {
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
	if !h.reported("rows examined") {
		t.Errorf("probe produced no comparison report: %v", h.report)
	}
	if got := h.pty.snapshotResizes(); len(got) != 2 {
		t.Errorf("probe resizes = %v, want two", got)
	}
}

// If the narrow frame does not match the display -- the screen moved under
// the pass -- nothing is stamped, rather than stamping the wrong rows.
func TestWinReflowOracleAbortsOnMismatch(t *testing.T) {
	h := newReflowHarness(t, winReflowOracle, 40, 12)
	h.playScenario()
	// Corrupt the display's idea of one row after the fake rendered it.
	h.pf.termView.mu.Lock()
	h.pf.termView.Lines[5][0].Char = 'X'
	h.pf.termView.mu.Unlock()
	before := h.flags()
	h.pf.runReflowOracle()
	h.waitForOracle(2 * time.Second)
	if !h.reported("mismatch") {
		t.Fatalf("expected a mismatch report, got %v", h.report)
	}
	if after := h.flags(); strings.Join(boolsToStrings(after), "") != strings.Join(boolsToStrings(before), "") {
		t.Errorf("a mismatched pass stamped rows: %v -> %v", before, after)
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
