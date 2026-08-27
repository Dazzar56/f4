//go:build windows

package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	baseW = 40
	baseH = 12
	// The width the reflow oracle (TERMINAL_LEDGER §3.3 a) wants to borrow.
	oracleW = 4000
)

var (
	// Exactly the console width: a hard break that must not be read as a wrap.
	exactLine = strings.Repeat("A", baseW)
	// Half a row over: this one really wraps.
	overLine  = strings.Repeat("B", baseW+20)
	exactMark = "AAAAAAAA"
	overMark  = "BBBBBBBB"
)

// ptyBroken is set when a pseudoconsole session produced no output at all.
// Everything downstream measures the shell's bytes, so there is nothing
// honest left to report and the remaining sections are skipped rather than
// filled with zeros that read like findings.
var ptyBroken string

func newSession(w *writer, flags uint32, cols, rows int16) (*pty, []byte, error) {
	cmdline := "cmd.exe"
	if c := os.Getenv("COMSPEC"); c != "" {
		cmdline = c
	}
	p, err := newPTY(flags, cols, rows, cmdline)
	if err != nil {
		return nil, nil, err
	}
	banner := p.drain(500*time.Millisecond, 5*time.Second)
	if len(banner) == 0 {
		// Some hosts hold the first frame until the client is nudged.
		p.send("\r")
		banner = p.drain(700*time.Millisecond, 3*time.Second)
	}
	if len(banner) == 0 {
		code, alive := p.exitCode()
		all := snapshotProcesses()
		w.printf("!! the pseudoconsole produced no bytes at all.\n")
		w.printf("   cmdline=%q shell pid=%d alive=%v exitcode=%d (%#x)\n",
			cmdline, p.pid, alive, code, code)
		w.printf("   children of the probe now: %s\n",
			describeProcs(childrenOf(uint32(os.Getpid()), all)))
		p.close()
		return nil, nil, fmt.Errorf("no output from the pseudoconsole (shell alive=%v exit=%#x)", alive, code)
	}
	return p, banner, nil
}

// ---------------------------------------------------------------------------
// 2. Flags, and the line structure of the live stream
// ---------------------------------------------------------------------------

func probeFlags(w *writer) {
	w.section("2. CreatePseudoConsole flags, and where the line breaks are")
	w.printf("0x0 is what f4 uses. 0x2 is PSEUDOCONSOLE_RESIZE_QUIRK (P5). 0x8 is\n")
	w.printf("passthrough mode, documented for Windows 11 22H2+ (P10).\n")
	w.printf("Two payloads: one line of exactly %d columns (a hard break that must\n", baseW)
	w.printf("not be mistaken for a wrap) and one of %d (a real wrap).\n", len(overLine))

	type result struct {
		ok            bool
		live          []byte
		wideRepaint   []byte
		narrowRepaint []byte
	}
	results := map[uint32]result{}

	for _, flags := range []uint32{0x0, 0x2, 0x8} {
		name := map[uint32]string{0x0: "default", 0x2: "RESIZE_QUIRK", 0x8: "PASSTHROUGH"}[flags]
		w.sub(fmt.Sprintf("flags=%#x (%s)", flags, name))
		w.step("  flags %#x (%s)...", flags, name)

		p, banner, err := newSession(w, flags, baseW, baseH)
		if err != nil {
			w.printf("CreatePseudoConsole/spawn failed: %v\n", err)
			w.summary(fmt.Sprintf("conpty.flag_%#x.usable", flags), "no ("+err.Error()+")")
			if flags == 0 {
				ptyBroken = err.Error()
			}
			results[flags] = result{}
			continue
		}
		w.summary(fmt.Sprintf("conpty.flag_%#x.usable", flags), "yes")

		// One grid fed with everything, so absolute cursor moves land where
		// ConPTY means them to.
		g := NewGrid(baseW, baseH)
		g.Feed(banner)
		var live []byte
		for _, payload := range []string{exactLine, overLine} {
			p.line("echo " + payload)
			chunk := p.drain(400*time.Millisecond, 3*time.Second)
			live = append(live, chunk...)
			g.Feed(chunk)
		}
		ve := AnalyzeLine(g, exactMark)
		vo := AnalyzeLine(g, overMark)
		w.printf("live stream: %d bytes (banner %d)\n", len(live), len(banner))
		w.printf("%s", g.Report(24))
		w.printf("exact-%d line : rows=%d ends=%s softwrap=%v hardcrlf=%v\n",
			baseW, ve.Rows, endsList(g, exactMark), ve.SoftWrap, ve.HardCRLF)
		w.printf("over-%d line  : rows=%d ends=%s softwrap=%v hardcrlf=%v\n",
			len(overLine), vo.Rows, endsList(g, overMark), vo.SoftWrap, vo.HardCRLF)
		w.printf("raw:\n%s\n", Clip(Escape(live), 2000))

		// Widen. The repaint is a frame of its own, so it gets a fresh grid.
		p.resize(100, baseH)
		wide, wideFirst, wideLast := p.drainTimed(400*time.Millisecond, 3*time.Second)
		gw := NewGrid(100, baseH)
		gw.Feed(wide)
		vw := AnalyzeLine(gw, overMark)
		w.printf("\nresize %d->100: %d bytes, first byte after %v, last after %v\n",
			baseW, len(wide), wideFirst.Round(time.Millisecond), wideLast.Round(time.Millisecond))
		w.printf("  civis=%v cnorm=%v winops=%v; the %d-column line is now %d row(s)\n",
			frameHidden(wide), frameShown(wide), gw.WinOps, len(overLine), vw.Rows)
		w.printf("%s", gw.Report(16))
		w.printf("raw:\n%s\n", Clip(Escape(wide), 2200))

		// Narrow back. This is the frame that decides the reflow question:
		// does ConPTY re-break the long line itself, or hand it over whole and
		// let the terminal wrap it?
		p.resize(baseW, baseH)
		back, _, _ := p.drainTimed(400*time.Millisecond, 3*time.Second)
		gb := NewGrid(baseW, baseH)
		gb.Feed(back)
		vbe := AnalyzeLine(gb, exactMark)
		vb := AnalyzeLine(gb, overMark)
		w.printf("\nresize 100->%d: %d bytes; the long line is %d row(s), ends=%s\n",
			baseW, len(back), vb.Rows, endsList(gb, overMark))
		w.printf("  exact-%d output: rows=%d first-end=%s hardcrlf=%v EL-on-break=%v hint-would-join=%v\n",
			baseW, vbe.Rows, printableEnd(vbe.FirstEnd), vbe.HardCRLF, vbe.ELOnBreak,
			vbe.HardCRLF && !vbe.ELOnBreak)
		w.printf("  softwrap=%v hardcrlf=%v EL-on-wrapped=%v EL-on-break=%v\n",
			vb.SoftWrap, vb.HardCRLF, vb.ELOnWrapped, vb.ELOnBreak)
		w.printf("%s", gb.Report(16))
		w.printf("raw:\n%s\n", Clip(Escape(back), 2200))

		if flags == 0 {
			w.summary("wrap.live.exact_width_line_ends", endsList(g, exactMark))
			w.summary("wrap.live.exact_width_output_end", printableEnd(ve.FirstEnd))
			w.summary("wrap.live.exact_width_output_hard_crlf", yesno(ve.HardCRLF))
			w.summary("wrap.live.long_line_ends", endsList(g, overMark))
			w.summary("wrap.live.soft_wrap_in_stream", yesno(vo.SoftWrap))
			w.summary("wrap.live.hard_crlf_at_wrap", yesno(vo.HardCRLF))
			w.summary("wrap.repaint.long_line_ends", endsList(gb, overMark))
			w.summary("wrap.repaint.exact_width_output_end", printableEnd(vbe.FirstEnd))
			w.summary("wrap.repaint.exact_width_output_hard_crlf", yesno(vbe.HardCRLF))
			w.summary("wrap.repaint.exact_width_output_el", yesno(vbe.ELOnBreak))
			w.summary("wrap.repaint.exact_width_hint_would_join", yesno(vbe.HardCRLF && !vbe.ELOnBreak))
			w.summary("wrap.repaint.soft_wrap_in_stream", yesno(vb.SoftWrap))
			w.summary("wrap.repaint.hard_crlf_at_wrap", yesno(vb.HardCRLF))
			w.summary("wrap.el_on_wrapped_row", yesno(vb.ELOnWrapped || vo.ELOnWrapped))
			w.summary("wrap.el_on_broken_row", yesno(vb.ELOnBreak || vo.ELOnBreak))
			w.summary("repaint.civis_delimited", yesno(frameHidden(wide) && frameShown(wide)))
			w.summary("repaint.rejoins_lines", yesno(vw.Rows == 1))
			w.summary("repaint.reports_winops", fmt.Sprintf("%v", gw.WinOps))
		}
		results[flags] = result{
			ok:            true,
			live:          append([]byte(nil), live...),
			wideRepaint:   append([]byte(nil), wide...),
			narrowRepaint: append([]byte(nil), back...),
		}
		p.line("exit")
		p.drain(200*time.Millisecond, 1*time.Second)
		p.close()
	}

	// P5/P10 are comparisons, not acceptances: does either flag change anything?
	base := results[0x0]
	for _, f := range []uint32{0x2, 0x8} {
		r := results[f]
		if !base.ok || !r.ok {
			continue
		}
		liveSame := bytes.Equal(base.live, r.live)
		wideSame := bytes.Equal(base.wideRepaint, r.wideRepaint)
		narrowSame := bytes.Equal(base.narrowRepaint, r.narrowRepaint)
		resizeSame := wideSame && narrowSame
		w.printf("\nbyte comparison default vs flag %#x: live=%v, widen=%v, narrow=%v\n",
			f, liveSame, wideSame, narrowSame)
		w.printf("  bytes: live %d/%d, widen %d/%d, narrow %d/%d\n",
			len(base.live), len(r.live), len(base.wideRepaint), len(r.wideRepaint),
			len(base.narrowRepaint), len(r.narrowRepaint))
		w.summary(fmt.Sprintf("conpty.flag_%#x.changes_live_output", f), yesno(!liveSame))
		w.summary(fmt.Sprintf("conpty.flag_%#x.changes_resize_output", f), yesno(!resizeSame))
	}
}

func printableEnd(end string) string {
	if end == "" {
		return "(none)"
	}
	return end
}

func frameHidden(b []byte) bool {
	return strings.Contains(string(b[:min(len(b), 40)]), "\x1b[?25l")
}

func frameShown(b []byte) bool {
	return strings.Contains(string(b[max(0, len(b)-40):]), "\x1b[?25h")
}

// ---------------------------------------------------------------------------
// 3. Reflow: scrollback and the oracle
// ---------------------------------------------------------------------------

func probeReflow(w *writer) {
	w.section("3. Reflow: the two step-zero questions from TERMINAL_LEDGER §3.3")
	if ptyBroken != "" {
		w.printf("skipped: %s\n", ptyBroken)
		w.summary("section3", "skipped, no pseudoconsole output")
		return
	}
	w.step("  reflow: scrollback, wide-resize oracle...")

	p, banner, err := newSession(w, 0, baseW, baseH)
	if err != nil {
		w.printf("session failed: %v\n", err)
		return
	}
	defer p.close()
	w.printf("banner = %d bytes\n", len(banner))

	// (1) Does conhost keep scrollback under ConPTY beyond the viewport?
	w.sub("scrollback: 30 lines in a 12-row console, then widen")
	p.line(`for /l %i in (1,1,30) do @echo LINE_%i_ZZ`)
	printed := p.drain(500*time.Millisecond, 8*time.Second)
	w.printf("printing 30 lines emitted %d bytes\n", len(printed))

	p.resize(100, baseH)
	frame, _, _ := p.drainTimed(500*time.Millisecond, 4*time.Second)
	scrolledOff := strings.Contains(string(frame), "LINE_3_ZZ") ||
		strings.Contains(string(frame), "LINE_5_ZZ")
	gs := NewGrid(100, baseH)
	gs.Feed(frame)
	w.printf("repaint after widening: %d bytes, %d rows written\n", len(frame), countWritten(gs))
	w.printf("contains a line that had scrolled off (LINE_3/LINE_5): %v\n", scrolledOff)
	w.printf("raw:\n%s\n", Clip(Escape(frame), 2000))
	w.summary("repaint.includes_scrollback", yesno(scrolledOff))
	p.resize(baseW, baseH)
	p.drain(400*time.Millisecond, 3*time.Second)

	// (1b) Which resizes repaint, and which repaints say their size. Both
	// answers decided the scrollback bug on 22000 and both were guessed
	// wrong first: a height-only change repaints (6.15), and a call for the
	// size ConPTY already has repaints too, without the size report (6.16).
	// A build that behaves differently here is exactly what the ledger's
	// portability question is about.
	w.sub("which resizes repaint: height-only, then the same size again")
	report := func(label string, frame []byte) {
		sized := strings.Contains(string(frame), "\x1b[8;")
		delimited := frameHidden(frame) && frameShown(frame)
		w.printf("%s: %d bytes, delimited frame=%v, carries ESC[8;rows;cols t=%v\n",
			label, len(frame), delimited, sized)
		w.summary("repaint."+label+".frame", yesno(delimited))
		w.summary("repaint."+label+".size_report", yesno(sized))
	}
	p.resize(baseW, baseH-2)
	f1, _, _ := p.drainTimed(500*time.Millisecond, 4*time.Second)
	report("height_only", f1)
	p.resize(baseW, baseH-2)
	f2, _, _ := p.drainTimed(500*time.Millisecond, 4*time.Second)
	report("same_size", f2)
	p.resize(baseW, baseH)
	p.drain(400*time.Millisecond, 3*time.Second)

	// (2) The oracle: borrow a very wide pseudoconsole for a moment.
	w.sub(fmt.Sprintf("oracle: one wrapped line, then resize to %d columns", oracleW))
	p.line("echo " + overLine)
	p.drain(400*time.Millisecond, 3*time.Second)

	if err := p.resize(oracleW, baseH); err != nil {
		w.printf("ResizePseudoConsole(%d,%d) FAILED: %v\n", oracleW, baseH, err)
		w.summary("oracle.wide_resize_accepted", "no")
	} else {
		wide, first, last := p.drainTimed(400*time.Millisecond, 5*time.Second)
		gw := NewGrid(oracleW, baseH)
		gw.Feed(wide)
		v := AnalyzeLine(gw, overMark)
		w.printf("wide repaint: %d bytes; first byte after %v, complete after %v\n",
			len(wide), first.Round(time.Millisecond), last.Round(time.Millisecond))
		w.printf("the %d-character line came back as %d row(s); civis=%v cnorm=%v\n",
			len(overLine), v.Rows, frameHidden(wide), frameShown(wide))
		w.printf("raw (clipped):\n%s\n", Clip(Escape(wide), 1600))
		w.summary("oracle.wide_resize_accepted", "yes")
		w.summary("oracle.wide_bytes", fmt.Sprintf("%d", len(wide)))
		w.summary("oracle.wide_rejoins", yesno(v.Rows == 1))
		w.summary("oracle.wide_civis_delimited", yesno(frameHidden(wide) && frameShown(wide)))
		w.summary("oracle.wide_latency", fmt.Sprintf("first %v, done %v",
			first.Round(time.Millisecond), last.Round(time.Millisecond)))

		var back []byte
		var bfirst, blast time.Duration
		if err := p.resize(baseW, baseH); err != nil {
			w.printf("resize back FAILED: %v\n", err)
		} else {
			back, bfirst, blast = p.drainTimed(400*time.Millisecond, 4*time.Second)
		}
		gb := NewGrid(baseW, baseH)
		gb.Feed(back)
		vb := AnalyzeLine(gb, overMark)
		w.printf("resize back: %d bytes; first after %v, done after %v; the line is %d row(s), ends=%s\n",
			len(back), bfirst.Round(time.Millisecond), blast.Round(time.Millisecond),
			vb.Rows, endsList(gb, overMark))
		w.printf("raw (clipped):\n%s\n", Clip(Escape(back), 1600))
		w.summary("oracle.roundtrip_latency", (last + blast).Round(time.Millisecond).String())
	}

	// (3) Narrowing, which nothing has measured yet.
	w.sub("narrowing 40 -> 20")
	p.resize(20, baseH)
	narrow, _, _ := p.drainTimed(400*time.Millisecond, 3*time.Second)
	gn := NewGrid(20, baseH)
	gn.Feed(narrow)
	vn := AnalyzeLine(gn, overMark)
	w.printf("narrow repaint: %d bytes; the long line is %d row(s), ends=%s, civis=%v\n",
		len(narrow), vn.Rows, endsList(gn, overMark), frameHidden(narrow))
	w.printf("%s", gn.Report(14))
	w.printf("raw (clipped):\n%s\n", Clip(Escape(narrow), 1400))
	w.summary("repaint.narrowing_bytes", fmt.Sprintf("%d", len(narrow)))
	w.summary("repaint.narrowing_long_line_ends", endsList(gn, overMark))
	p.resize(baseW, baseH)
	p.drain(300*time.Millisecond, 2*time.Second)

	p.line("exit")
	p.drain(200*time.Millisecond, 1*time.Second)
}

func countWritten(g *Grid) int {
	n := 0
	for _, r := range g.Rows() {
		if r.Written > 0 {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// 4. cmd.exe session behaviour
// ---------------------------------------------------------------------------

func probeCmdSession(w *writer) {
	w.section("4. cmd.exe: OSC 133 marks, the console title, batch echo, children")
	if ptyBroken != "" {
		w.printf("skipped: %s\n", ptyBroken)
		w.summary("section4", "skipped, no pseudoconsole output")
		return
	}
	w.step("  cmd session: marks, title, batch, children...")

	p, banner, err := newSession(w, 0, 80, 25)
	if err != nil {
		w.printf("session failed: %v\n", err)
		return
	}
	defer p.close()
	w.printf("shell pid = %d, banner = %d bytes\n", p.pid, len(banner))
	w.summary("cmd.pid", fmt.Sprintf("%d", p.pid))

	all := snapshotProcesses()
	var kids []procInfo
	ourKids := describeProcs(childrenOf(uint32(os.Getpid()), all))
	w.printf("children of this probe (pid %d): %s\n", os.Getpid(), ourKids)
	w.printf("children of the shell           : %s\n", describeProcs(childrenOf(p.pid, all)))
	lk := strings.ToLower(ourKids)
	w.summary("children.conhost_is_our_child",
		yesno(strings.Contains(lk, "conhost") || strings.Contains(lk, "openconsole")))

	// P3/C4: the title. Two separate questions -- is it in the VT stream, and
	// is it readable from outside the pseudoconsole.
	w.sub("console title")
	p.line("title F4PROBE_TITLE_XYZ")
	titleOut := p.drain(400*time.Millisecond, 3*time.Second)
	gt := NewGrid(80, 25)
	gt.Feed(titleOut)
	w.printf("OSC strings in the stream: %v\n", gt.OSCs)
	w.printf("raw:\n%s\n", Clip(Escape(titleOut), 700))
	wins := windowsOfPID(p.pid)
	if len(wins) == 0 {
		w.printf("top-level windows owned by the shell: (none)\n")
	}
	for _, s := range wins {
		w.printf("  window: %s\n", s)
	}
	w.summary("title.forwarded_in_stream", yesno(anyHasPrefix(gt.OSCs, "0;")))
	w.summary("title.readable_from_outside", yesno(len(wins) > 0))

	// C4 again, and this is the interesting one: while a command runs, cmd
	// rewrites the title to "<title> - <command>". If ConPTY forwards that,
	// it is a completion signal that owes nothing to PROMPT.
	w.sub("title while a command runs (a busy signal that does not depend on PROMPT)")
	timeoutStart := time.Now()
	p.line("timeout /t 3 /nobreak")
	observed, appearedAfter := waitForDirectChildren(p.pid, 2*time.Second)
	running := p.drain(150*time.Millisecond, 500*time.Millisecond)
	gr := NewGrid(80, 25)
	gr.Feed(running)
	w.printf("children during `timeout`: %s (observed after %v)\n",
		describeProcs(observed), appearedAfter.Round(time.Millisecond))
	w.printf("OSC while running: %v\n", gr.OSCs)
	w.summary("timeout.child_seen", yesno(len(observed) != 0))
	if len(observed) == 0 {
		w.printf("!! no child appeared within 2s; stopping section 4 so later zero mark counts\n")
		w.printf("   cannot be mistaken for measurements. The PROMPT command was not sent.\n")
		w.summary("timeout.child_exit_confirmed", "no (child was not observed)")
		w.summary("section4.after_timeout", "skipped, child lifecycle not observed")
		return
	}
	remaining, waited := waitForProcessesGone(observed, 8*time.Second)
	exitAfter := time.Since(timeoutStart)
	exitConfirmed := len(remaining) == 0
	w.printf("timeout child exit: confirmed=%v after %v (waited %v), remaining=%s\n",
		exitConfirmed, exitAfter.Round(time.Millisecond), waited.Round(time.Millisecond),
		describeProcs(remaining))
	w.summary("timeout.child_exit_confirmed", yesno(exitConfirmed))
	w.summary("timeout.child_lifetime", exitAfter.Round(time.Millisecond).String())
	if !exitConfirmed {
		w.printf("!! timeout child was still alive after 8s; stopping section 4.\n")
		w.printf("   The PROMPT command was not sent, so later results are not fabricated.\n")
		w.summary("section4.after_timeout", "skipped, child did not exit")
		return
	}
	rest := p.drain(700*time.Millisecond, 3*time.Second)
	gd := NewGrid(80, 25)
	gd.Feed(rest)
	w.printf("OSC after it finished: %v\n", gd.OSCs)
	w.summary("children.during_timeout", describeProcs(observed))
	w.summary("title.command_form_while_running", yesno(anyContains(gr.OSCs, " - ")))
	w.summary("title.restored_when_done",
		yesno(anyHasPrefix(gd.OSCs, "0;") && !anyContains(gd.OSCs, " - ")))

	// P2: the order of the OSC 133 mark and the prompt text it belongs to.
	w.sub("OSC 133 marks in PROMPT")
	p.line(`prompt $E]133;A$E\$P$G$E]133;B$E\`)
	p.drain(400*time.Millisecond, 3*time.Second)
	p.line("echo hi")
	marked := p.drain(400*time.Millisecond, 3*time.Second)
	s := string(marked)
	iB := strings.Index(s, "\x1b]133;B")
	iPrompt := strings.LastIndex(s, ">")
	order := "unknown"
	if iB >= 0 && iPrompt >= 0 {
		if iB < iPrompt {
			order = "mark_before_prompt_text"
		} else {
			order = "prompt_text_before_mark"
		}
	}
	w.printf("marks pass through: A=%v B=%v; order = %s\n",
		strings.Contains(s, "\x1b]133;A"), iB >= 0, order)
	w.printf("raw:\n%s\n", Clip(Escape(marked), 1400))
	w.summary("osc133.passthrough", yesno(strings.Contains(s, "\x1b]133;A") && iB >= 0))
	w.summary("osc133.order", order)

	// C1/C8: what a batch does to the prompt.
	for _, bat := range []struct{ name, body, note string }{
		{"f4probe_echoon.bat", "echo B1\r\necho B2\r\necho B3\r\n", "ECHO ON: expect a prompt in front of every line (C1)"},
		{"f4probe_echooff.bat", "@echo off\r\necho B1\r\necho B2\r\n", "ECHO OFF: expect no prompts between lines"},
		{"f4probe_promptoff.bat", "prompt $P$G\r\necho B1\r\necho B2\r\n", "the batch resets PROMPT: expect the marks to vanish (C8)"},
	} {
		path := filepath.Join(os.TempDir(), bat.name)
		if err := os.WriteFile(path, []byte(bat.body), 0644); err != nil {
			w.printf("cannot write %s: %v\n", path, err)
			continue
		}
		w.sub("batch: " + bat.name)
		w.printf("%s\n", bat.note)
		p.line(`"` + path + `"`)
		out := p.drain(600*time.Millisecond, 6*time.Second)
		gbat := NewGrid(80, 25)
		gbat.Feed(out)
		marks := strings.Count(string(out), "\x1b]133;B")
		w.printf("%d bytes, %d OSC 133;B marks; OSC seen: %v\n", len(out), marks, gbat.OSCs)
		w.printf("raw:\n%s\n", Clip(Escape(out), 1800))
		w.summary("batch."+strings.TrimSuffix(bat.name, ".bat")+".marks", fmt.Sprintf("%d", marks))
		w.summary("batch."+strings.TrimSuffix(bat.name, ".bat")+".title_shows_command",
			yesno(anyContains(gbat.OSCs, " - ")))
		os.Remove(path)
		if strings.Contains(bat.name, "promptoff") {
			p.line(`prompt $E]133;A$E\$P$G$E]133;B$E\`)
			p.drain(400*time.Millisecond, 2*time.Second)
		}
	}

	// C2: a batch step that waits is interpreted in-process -- no child at all.
	w.sub("children while a pure-builtin batch step is waiting (C2)")
	pausePath := filepath.Join(os.TempDir(), "f4probe_pause.bat")
	if err := os.WriteFile(pausePath, []byte("@echo off\r\necho WAITING\r\npause\r\n"), 0644); err == nil {
		p.line(`"` + pausePath + `"`)
		out := p.drain(500*time.Millisecond, 4*time.Second)
		gp := NewGrid(80, 25)
		gp.Feed(out)
		all = snapshotProcesses()
		kids = childrenOf(p.pid, all)
		w.printf("while `pause` is waiting, children of the shell: %s\n", describeProcs(kids))
		w.printf("OSC while waiting: %v\n", gp.OSCs)
		w.printf("screen:\n%s\n", Clip(Escape(out), 600))
		w.summary("children.during_batch_pause", describeProcs(kids))
		w.summary("title.command_form_during_batch_pause", yesno(anyContains(gp.OSCs, " - ")))
		p.send(" ")
		p.drain(400*time.Millisecond, 3*time.Second)
		os.Remove(pausePath)
	}

	// C7: a nested cmd looks exactly like the outer one. The first run read
	// this too early and caught four bytes; give it room and confirm the child.
	w.sub("nested cmd")
	p.line("cmd")
	nested := p.drain(800*time.Millisecond, 6*time.Second)
	all = snapshotProcesses()
	kids = childrenOf(p.pid, all)
	nestedCmd := false
	for _, k := range kids {
		if strings.EqualFold(k.Name, "cmd.exe") {
			nestedCmd = true
		}
	}
	w.printf("children after `cmd`: %s\n", describeProcs(kids))
	w.printf("marks in the nested prompt: %v\n", strings.Contains(string(nested), "\x1b]133;"))
	w.printf("raw:\n%s\n", Clip(Escape(nested), 1200))
	w.summary("children.nested_cmd", describeProcs(kids))
	w.summary("nested_cmd.is_a_child", yesno(nestedCmd))
	w.summary("nested_cmd.inherits_marks", yesno(strings.Contains(string(nested), "\x1b]133;")))
	p.line("exit")
	p.drain(600*time.Millisecond, 4*time.Second)

	// C5: cmd does not wait for a GUI application. Modern Notepad may be
	// brokered outside cmd's descendant tree, so record both the shell tree and
	// the system-wide PID delta. Never touch a Notepad process that predated the
	// probe.
	w.sub("notepad (GUI application; shell tree and global PID delta)")
	beforeNotepad := snapshotProcesses()
	preexisting := processesNamed(beforeNotepad, "notepad.exe")
	w.printf("pre-existing Notepad processes: %s\n", describeProcs(preexisting))
	w.summary("notepad.preexisting", describeProcs(preexisting))
	if len(preexisting) != 0 {
		w.printf("scenario skipped: refusing to mix with or close a user's existing Notepad\n")
		w.summary("notepad.scenario", "skipped (pre-existing Notepad process)")
	} else {
		notePath := filepath.Join(os.TempDir(), fmt.Sprintf("f4probe_notepad_%d.txt", os.Getpid()))
		if err := os.WriteFile(notePath, []byte("f4probe temporary file\r\n"), 0644); err != nil {
			w.printf("cannot create Notepad control file: %v\n", err)
			w.summary("notepad.scenario", "skipped (cannot create control file)")
		} else {
			w.step("  a Notepad window will flash on screen; only a newly observed PID may be closed")
			p.line(`notepad.exe "` + notePath + `"`)
			p.drain(800*time.Millisecond, 2*time.Second)
			afterNotepad, newGlobal, observedAfter := waitForNewProcessesNamed(
				beforeNotepad, "notepad.exe", 5*time.Second)
			kids = childrenOf(p.pid, afterNotepad)
			deep := descendantsOf(p.pid, afterNotepad, 3)
			w.printf("children of the shell: %s\n", describeProcs(kids))
			w.printf("descendants (3 deep) : %s\n", describeProcs(deep))
			w.printf("new Notepad processes anywhere after %v: %s\n",
				observedAfter.Round(time.Millisecond), describeProcs(newGlobal))
			for _, np := range newGlobal {
				w.printf("  chain: %s\n", describeProcessChain(np.PID, afterNotepad))
				for _, win := range windowsOfPID(np.PID) {
					w.printf("  window: %s\n", win)
				}
			}
			w.summary("children.notepad", describeProcs(kids))
			w.summary("notepad.new_global", describeProcs(newGlobal))
			w.summary("notepad.new_process_observed_after", observedAfter.Round(time.Millisecond).String())
			w.summary("notepad.outside_shell_tree", yesno(len(newGlobal) != 0 && len(deep) == 0))

			terminateRequested := 0
			for _, np := range newGlobal {
				if h := openProcess(processTerminate, np.PID); h != 0 {
					if err := syscall.TerminateProcess(h, 0); err == nil {
						terminateRequested++
					}
					syscall.CloseHandle(h)
				}
			}
			remaining, _ := waitForProcessesGone(newGlobal, 2*time.Second)
			closed := len(newGlobal) - len(remaining)
			w.printf("new Notepad processes: %d; terminate requested: %d; closed: %d; remaining: %s\n",
				len(newGlobal), terminateRequested, closed, describeProcs(remaining))
			w.summary("notepad.closed_new_processes", fmt.Sprintf("%d/%d", closed, len(newGlobal)))
			if len(newGlobal) == 0 {
				w.printf("!! no new Notepad PID was found; nothing was closed for safety\n")
				w.summary("notepad.scenario", "ran, but no new Notepad PID was observable")
			} else if len(remaining) != 0 {
				w.summary("notepad.scenario", "ran, cleanup incomplete")
			} else {
				w.summary("notepad.scenario", "ran, all new Notepad PIDs closed")
			}
			if err := os.Remove(notePath); err != nil {
				w.printf("temporary Notepad file cleanup failed: %v\n", err)
			}
			p.drain(400*time.Millisecond, 2*time.Second)
		}
	}

	p.line("exit")
	p.drain(300*time.Millisecond, 2*time.Second)
}

func anyContains(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func anyHasPrefix(list []string, prefix string) bool {
	for _, s := range list {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func endsList(g *Grid, marker string) string {
	var out []string
	for _, r := range g.Rows() {
		if strings.Contains(r.Text, marker) {
			e := r.EndedBy
			if e == EndNone {
				e = "-"
			}
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return "(marker not found)"
	}
	return strings.Join(out, ",")
}
