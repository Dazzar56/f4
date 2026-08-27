# Terminal ledger: what is known, what is open, what to do next

This is the index. It exists because the knowledge below was earned across
one long conversation, two field testers, three probes and eighteen patches,
and a model or a person starting fresh will see the repository, not the chat.
Every entry names where it came from and whether the code already acts on it.

Companion documents, each deeper on its topic:

- `TERMINAL.md` — architecture of the terminal.
- `TERMINAL_WINDOWS.md` — the Windows bug list, §3.1–3.4 field results.
- `TERMINAL_CONPTY_FINDINGS.md` — what the ConPTY probe measured.
- `TERMINAL_REFLOW.md` — how other terminals reflow; far2l in detail.

Sources: **F** = field report from a tester; **P** = probe log
(`tools/conptyprobe`, `f4-probe.ps1`, `f4probe` in issue #425); **C** = read
from code or a spec; **M** = measured here with a test against the parser.

Two Windows builds have been probed end to end: **10.0.19045.7663** and
**10.0.22000.2538**. Where they disagree the entry says so; an entry with no
build named is the same on both.

## 1. Findings

### 1.1. cmd.exe

| # | Finding | Source | Status |
| --- | --- | --- | --- |
| C1 | With ECHO on, cmd prints `PROMPT` in front of every batch line, so any completion mark placed in `PROMPT` is forged by the first line of a batch (#409). The synchronized 22000 probe counted four prompt-end marks for a three-line ECHO-on batch, versus one final mark with ECHO off. | C, F, P | handled: settled-prompt rule |
| C2 | cmd interprets batch files in-process: no child appears for the batch itself, only for programs it runs. | C, P | handled |
| C3 | cmd prints CRLF then the prompt after a command; nothing before the prompt after `cls`. | C | noted (sync cleanup design) |
| C4 | cmd's console title takes the form `<title> - <command>` while running something. | C, P | **reopened** (O10). P3 says the title cannot be read by enumerating windows; P18 says it arrives in the VT stream anyway. The veto was dropped on the strength of the wrong one of those two. |
| C5 | cmd does not wait for a GUI program. Its process topology is not stable enough to say that `notepad` is always cmd's child: one 22000 run opened a visible Notepad but found no descendant, while both paired version-5 runs found a direct child plus its child and safely closed both new PIDs. | C, F, P | handled for session completion: an absent child cannot hold the terminal, and an enumerated GUI child is ignored; do not use Notepad parentage as a probe invariant |
| C6 | `%VAR%` with ESC in it expands inside a typed line (`$E` in PROMPT works; env-manager's `type` of a file holding `ESC]133;E` works). Basis for any self-erasing cleanup line. | C | design only |
| C7 | A nested `cmd` prints the same prompt and accepts the same typed lines; PowerShell rejects `cd /d`. Hence only `cmd.exe` is exempt from the child veto. | C, F | handled |
| C8 | A batch that runs `prompt $P$G` strips the marks from every later prompt. The synchronized 22000 run saw one mark before the reset took effect and none for either later line or the final prompt. | C, P | **open** (O4): needs the title path measured in P18/P19 |

### 1.2. ConPTY

| # | Finding | Source | Status |
| --- | --- | --- | --- |
| P1 | OSC 133 marks with any parameters pass through ConPTY verbatim. The result was repeated on 22000 only after `timeout.exe` had been observed and its exit confirmed, so it is no longer downstream of a silence race. | C, F, P | relied on |
| P2 | On 10.0.19045 the mark is delivered **before** the prompt text that precedes it in cmd's output; on 10.0.26200 the text comes first. A child-synchronized 10.0.22000 run agrees with 19045. | F, M, P | handled: screen examined at settle time, both orders in the test model |
| P3 | The console title is **not** readable from outside a pseudoconsole: empty for every process, both builds. | P | title veto removed |
| P4 | `conhost.exe` instances are children of **f4**, not of cmd. A child enumeration on cmd's PID never sees them. | P | relied on |
| P5 | `PSEUDOCONSOLE_RESIZE_QUIRK` (0x2) is **accepted and ignored** on 19045.7663. On 22000.2538 an actual byte comparison, not a length comparison, found the live stream and both resize repaints identical to flag 0. | P | Windows live-grid reflow dropped |
| P6 | In the **resize repaint** on 19045 the wrap point carries a **hard CRLF**: no soft-wrap signal there. Full-width rows are padded to width with spaces. A row that ends short of the width is followed by `ESC[K`; a full row is not. Read P11-P13 before building on this: the CRLF is not universal. | P | the `ESC[K` clause is the exploitable hint (plan §3.3) |
| P7 | On resize ConPTY repaints the **whole viewport** as one frame: `ESC[?25l ESC[H`, each row + `ESC[K`, CRLF between rows, `ESC[r;cH ESC[?25h`. The line that was three rows at 40 columns comes back as **one row** at 100: ConPTY keeps lines whole and reflows its own buffer. | P | Windows live-grid reflow dropped: ConPTY does it |
| P8 | That repaint frame does **not** scroll f4's grid at any height 3–14. | M | creep-from-repaint hypothesis retracted |
| P9 | `ResizePseudoConsole(0,0)` must never be sent (TERMINAL.md rule 4). | C | guarded in `PTY.SetSize` |
| P10 | Passthrough mode (0x8) exists on Windows 11 22H2+. | C | measured, see P17 |
| P11 | ConPTY marks a wrap sometimes with a CRLF and sometimes with nothing, **within one build**. On 22000 the live stream breaks the long line with a real CRLF (`wrap, lf, lf`), and it also emits chunks with no line terminator at all, relying on the terminal's autowrap and then moving with an absolute CUP. Line structure must be read from a cursor model; a CRLF-only reading of the same bytes reported a two-row line as one 140-column row. | P | grid model, never a CRLF split |
| P12 | The **repaint** on 22000 does not break a long logical line: it is written whole into the narrow console and only its last row is followed by CRLF (`wrap, lf`); narrowing to 20 gives `wrap, wrap, lf, wrap, wrap, lf`. A cursor model therefore recovers joins in this repaint. | P | plan §3.3: the frame carries the structure, but P13 says the `ESC[K` shortcut alone is not exact |
| P13 | `ESC[K` appears after short broken rows and not after wrapped rows, but a hard-broken line of exactly 40 characters also arrived as a full row plus CRLF with no `ESC[K`. The probe's summary missed it because `wrap.el_on_broken_row` examined only the long-line marker; the raw grid records `len=40 end=lf ESC[K=false`. | P | plan §3.3 (b) remains the documented one-in-width guess; the oracle is required to correct viewport rows |
| P14 | The resize repaint opens with `ESC[?25l` **and an XTWINOPS size report**, `ESC[8;<rows>;<cols>t`, before `ESC[H`. Nothing in f4 reads it. It is an unambiguous "a repaint for this size starts here" marker, free of charge. | P | see O9 |
| P15 | The wide-resize oracle works on 22000: `ResizePseudoConsole(4000, 12)` is accepted, the repaint is 287 bytes, and it comes back with the long line rejoined. Two cursor-model runs measured first byte at 1-8 ms, frame complete at 8 ms, and round trip at 8-9 ms. | P | plan §3.3 (a) is not just feasible, it is nearly free |
| P16 | The repaint covers the **viewport only**. Thirty lines printed into a twelve-row console and then widened: the frame begins at LINE_21. conhost keeps no scrollback on ConPTY's behalf. | P | step-zero question 1 answered: the oracle recovers structure, never history |
| P17 | Passthrough (0x8) is accepted on 22000. An actual byte comparison found its live stream, wide repaint and narrow repaint identical to flag 0. From outside it is a no-op, exactly like the resize quirk. | P | P10 closed |
| P18 | cmd's `<title> - <command>` **is** forwarded through ConPTY as OSC 0, verbatim, e.g. `ESC]0;F4PROBE_TITLE_XYZ - timeout  /t 3 /nobreakBEL`; the bare title returned after the child's independently confirmed exit. | P | see O10; C4 reopened |
| P19 | The title carries the command form for ECHO-on, ECHO-off and PROMPT-resetting batches, including while an in-process `pause` has no child and no usable mark. It is the only signal measured so far that says "busy" during a batch's own waiting. | P | O10; this is what `IsBusy` and OSC 133 both miss (C2) |
| P20 | Paired version-5 runs on the same 22000 machine were correctly identified as classic conhost and Windows Terminal. Their complete cursor-model / resize / oracle sections were byte-identical and every cmd semantic verdict agreed. The outer hosts differed in DA1 (`?1;0c`, no sixel, versus `?61;4;...c`, sixel) and window topology, not in the probe's separately created child ConPTY. | P | host graphics detection and child ConPTY semantics are separate decisions; 24H2/25H2 still need measurement |
| P21 | The version-6 real-f4 matrix captured delimited wide/narrow frames in all ten `oracle`/`probe` passes, but the old matcher rejected all ten and stamped nothing. It assumed ConPTY repaint row *y* was display row *y*; the field log shows the repaint actually overlaps f4's combined recent history and viewport, with extra ConPTY-only private-sync rows. | P | fixed by exact consecutive-pair alignment against the local journal; standalone oracle mechanics remain valid |

### 1.3. f4 itself, Windows path

| # | Finding | Source | Status |
| --- | --- | --- | --- |
| W1 | `Esc` is gated on `EscToggle` → `!isPtyBusy()`; `Ctrl+O` on `NoAltScreenApp`, which ignores busy. "Ctrl+O works, Esc does not" is the signature of `pf.executing` stuck true. | C, F | documented; used twice as a diagnostic |
| W2 | A settle check that compared against a snapshot taken at the mark failed every `dir` on 19045 (P2) and left the panels to a five-second fallback. | F | fixed |
| W3 | That five-second fallback also fired for any non-prompt screen, cutting off batch steps longer than five seconds (`timeout /t 10`, a `pause` being read). | F | fixed: busy screens are waited on without bound; the bound applies only to a prompt-shaped screen that will not hold still |
| W4 | With panels hidden and a nested `cmd` at its prompt, Enter in raw mode did nothing; after Ctrl+O, typing through f4's command line worked. | F | **open** (O3); sidestepped by returning the panels at the nested prompt |
| W5 | The directory sync (`cd /d "…" & rem f4_sync`) costs two or three rows of terminal per folder change; the stream excision erases the text, not the rows. Visible once at startup for one tester; on every folder change for another ("creep"). | F | **open** (O1) |
| W6 | `IsBusy()` (any child) is cached one second on Toolhelp; `ChildProcesses()` is uncached and only called while a prompt is examined. | C | as designed |
| W7 | The automatic real-f4 run completed startup, outer resizes, private-sync excision and nested-cmd Enter in `off`, `hint`, `oracle` and `probe`. Its original `complete` verdict only meant the scenario executed; P21 shows the oracle itself had not succeeded. | P | probe verdict fixed: oracle/probe require at least one safe journal match and report `incomplete` on `nothing stamped` |
| W8 | With the journal matcher in place the same matrix stamped real boundaries (5 `oracle` passes: 2, 2, 4, 4, 1 safe boundaries, 0 stale, 0 hint/oracle disagreements). The two `incomplete` verdicts in that run were runner faults: a fixed startup drain that f4's first flush outran, and a verdict that counted one safely-rejected oracle pass as a mode failure. | P | fixed in `tools/conptyprobe`: wait for f4's own `F4 STARTUP` line, and require one pass that stamped instead of zero that were rejected |

### 1.4. f4 itself, all platforms

| # | Finding | Source | Status |
| --- | --- | --- | --- |
| A1 | One `AnsiParser` serves every shell in the panel; device-query replies (DA, DSR, CPR) went to the local PTY regardless of who asked. `getActivePTY()` opens connections as a side effect, so the reply resolver had to be a separate side-effect-free lookup. | F | fixed (`replyTo`) |
| A2 | Keyboard protocol modes (`Win32InputMode`, `KittyFlags`, `ApplicationCursorKeys`) live on the shared `TerminalView` and outlive the session that set them; a far2l that died unreset left `;0;0;0;0_` in the remote shell. | F | mitigated: reset on session end; **open** for switching between two live shells (O5) |
| A3 | Unix managed commands are pasted into a brace group with the OSC 133 markers; a syntax error rejected the group, markers included, and a bare `>` hung the terminal. `eval` isolates it and also stops an unterminated quote from entering PS2. | F, M | fixed; invariant recorded in `TERMINAL.md` |
| A4 | Live-grid reflow is possible: far2l does it on the live grid. The cursor must ride through as an offset into the flattened text, not as coordinates. | C | shipped on Unix (width change only) |
| A5 | Trailing spaces on a soft-wrapped row are content (`ls -la` column alignment), never padding; trimming them glued columns. | F | fixed |
| A6 | Widening frees rows; the viewport must be refilled from `GridHistory` or earlier output is stranded off screen. | F | fixed |
| A7 | `vtui/colors.go` already declares `ExplicitLineBreak = 0x0400` and `ImportantLineChar = 0x0800`, far2l's values; nothing writes them. | C | bits reserved for plan §3.4 |
| A8 | far2l chooses reflow vs truncation by `ENABLE_PROCESSED_OUTPUT`; Unix has no equivalent visible to f4 (the mode lives on the slave side). Any substitute is a heuristic. | C | research item |

### 1.5. Other terminals

| Terminal | Reflow | Under ConPTY |
| --- | --- | --- |
| Windows Terminal | yes | reflows in its own hosting path; the paired 22000 run did not change the probe's separately created child ConPTY stream (P20), and the tested OS flag remained a no-op |
| WezTerm | yes | per-character guess whether ConPTY inserted a hard break (`enable_conpty_quirks`) — P6 shows why it is a guess |
| Alacritty | yes | pads with blank rows on resize |
| xterm.js | yes | throttles resize |
| far2l | yes | does not use ConPTY at all |
| ConEmu / Cmder | inherits conhost's | attaches to the console and reads the screen buffer through the Win32 API instead of a VT pipe; sees cells, not wrap flags. Not open to f4, which is not attached to the child's console |
| winpty (pre-ConPTY shim: VS Code, node-pty, mintty for native apps) | no | scraped the buffer and *guessed* line joins: a full-width row whose last cell is non-blank was treated as wrapped. Direct prior art for plan §3.3 (b), with the same one-in-W ambiguity |
| mintty (Git Bash, MSYS2) | yes | for Cygwin/MSYS ptys there is no conhost at all, so the signal is real; for native programs it goes through winpty or ConPTY and inherits their limits |
| Neovim/Vim `:terminal`, Rio, Contour, Warp | no scrollback reflow under ConPTY | all sit on ConPTY and stop where it stops |

None has recovered the soft-wrap signal, because the stream does not carry it.
The only two ideas in circulation are conhost's own reflow (ConEmu, and now
ConPTY itself per P7) and the full-row guess (winpty, WezTerm). Plan §3.3 (a)
is neither; it has no precedent in any of these, which is why it opens with a
probe.

## 2. Open items

| # | Item | Notes |
| --- | --- | --- |
| O1 | The creep (W5). | Sync-induced; onset reported at nightly 2462a68 — bisect candidate. Fix is the self-erasing cleanup line (C6, `TERMINAL_WINDOWS.md` §3 design). |
| O2 | Startup sync typed before the first prompt settles. | With O1. |
| O3 | Raw-mode Enter in a nested cmd (W4). | Unexplained. Needs a `--debug` log of the keystrokes as written. |
| O4 | A batch that resets PROMPT (C8). | The markless signal is now measured without the earlier race: OSC 0 carries the busy batch title and restores the bare title at completion (P18/P19). Implementation in `cmdShellSession` remains open. |
| O5 | Keyboard modes when switching between two live shells (A2). | Needs modes per session, not per `TerminalView`. |
| O6 | Reflow vs truncation without an alternate screen (A8). | Measure which programs actually suffer before adding a heuristic. |
| O7 | Windows 11 ConPTY: re-run the probe on a newer build. | Done for 10.0.22000.2538 (P11-P21), including forced conhost, explicit WT, the configured-default path and a real-f4 mode matrix. The run found the journal-alignment bug rather than a new ConPTY ambiguity. This build now has enough evidence for the implementation below; 24H2/25H2 remain a portability measurement, not a blocker for 22000. Nothing further about line structure can be learned from 22000: W8 shows the oracle stamping and agreeing with the hint wherever it could check it. |
| O8 | The wrap signal is build- and frame-dependent (P6 against P11/P12), while the exact-width hard break is ambiguous even on 22000 (P13). | Do not branch on a build number or promote `hint` to exact from the absence of `ESC[K`. Keep it marked as guessed; use the cursor-model oracle to overrule viewport guesses, and accept that rows already outside ConPTY's viewport cannot be made certain. `f4probe 5` gives the exact-width control its own verdict instead of folding it into the long-line summary. |
| O9 | The XTWINOPS report (P14) as the repaint delimiter. **Now a prerequisite, not a nicety (§6.8).** | Today the scratch-view routing keys on `ESC[?25l … ESC[?25h`. `ESC[8;h;wt` says the size as well, so a frame can be matched to the resize that caused it, and a frame that is not ours can be told apart. |
| O11 | Scrollback does not come back in the field (§6.7-§6.11). | **Root cause: the 2000-row `GridHistory` cap.** Narrowing produces more rows than it consumed, the cap evicts the oldest to the PieceTable, and the re-wrap reads only `GridHistory` -- so evicted rows can never come back, and a drag is hundreds of passes. The instrumented summary shows cells falling 107973 -> 54757 while all five counted loss points stay at zero; `REFLOW_DROP:` names the evicted rows. Fix candidates, unmeasured: bound the history in logical lines/cells instead of rows (smallest), raise the cap, or let the re-wrap read the PieceTable (only one that recovers already-evicted rows). Missing measurement: a counter on the `ScrollUp` cap site, two lines. |
| O12 | The oracle cannot fire when it is most needed. | It requires a settled prompt with no console child; a resize drag never offers one, and the rows being re-wrapped are exactly the ones no pass ever covered. Either accept that the hint carries the history (it agreed with the oracle everywhere measured, §6.6) and say so, or find a second safe moment to run a pass. |
| O10 | The console title as the busy signal (P18, P19). | Measurement is complete: it owes nothing to `PROMPT`, covers a childless batch `pause`, and was restored only after an independently observed child exit. Implementation is still open: track the OSC 0 baseline per session and use its busy/restored transition to close O4. |

## 3. Plan for the next session

The next session should not start by reading the chat; it should start here,
do these three things in this order, and update this file.

### 3.1. Mocks that reproduce Windows exactly

Today's test model (`windowsBuilds` in `cmd_session_test.go`) covers P2 and
P3 and the four observed children. It should become a full fake of the seams
f4 depends on, so that a change can be tried against every known behaviour
without a tester. One package, `cmd/f4/internal/conptyfake` or a
`fake_conpty_test.go`, exposing a scripted stream builder per build:

| Behaviour to reproduce | From |
| --- | --- |
| Prompt marks, with the mark-before-text order on 19045 and text-first on 26200 | P2 |
| Batch echo with ECHO on: prompt + command text + CRLF per line; none with `@echo off`; none after `prompt $P$G` | C1, C8 |
| `pause`, `set /p x=…>`, `timeout` output shapes; "Terminate batch job (Y/N)?" after Ctrl+C | C |
| cmd's CRLF before every prompt except after `cls` | C3 |
| Cooked-mode echo of a typed line, wrapping at the console width with immediate wrap | C |
| Wrapped rows delivered as hard CRLF, full rows padded to width, `ESC[K` only after short rows | P6 |
| The resize repaint frame, with wrapped lines rejoined | P7 |
| `RESIZE_QUIRK` as a no-op | P5 |
| No title readable through process/window enumeration | P3 |
| OSC 0 busy/restored title around an external command and batches, including a childless `pause` | P18, P19 |
| Child listings: `PING.EXE`, `timeout.exe`, nested `cmd.exe`, conhost absent; Notepad may be absent/brokered or a direct GUI child with another process below it | P4, C5 |
| OSC 133 passthrough with arbitrary parameters | P1 |
| Stream split at arbitrary byte boundaries (ConPTY chunking) | `ansi_parser_sync_test.go` |

Two builds are known; the fake should be a table so a third is one row.

### 3.2. Tests through the mocks

Every session test already runs under each build. Extend the same
`forEachBuild` pattern to:

- the directory sync end to end (typed, echoed, excised, prompt settled), which
  is where O1 lives;
- resize during a command and during a batch;
- the keyboard-mode leak (A2) and its reset;
- the reply routing (A1) with a remote shell driving;
- the creep itself: N folder changes must add zero rows to `GridHistory`
  once O1 is fixed — this is the acceptance test for the cleanup line.

A test that drives timers should either scale the delays (as today) or, where
the state machine allows, call the step function directly (as
`TestCmdSessionFlickeringPromptIsReleased` does). Two tests in this work were
wrong because they reasoned about timer interleaving; direct drive is safer.

### 3.3. Reflow on Windows: the elegant way around the ceiling

The ceiling (P5, P6): the stream has no soft-wrap signal and the flag that
would let a terminal reflow is a no-op. The viewport is not the problem —
ConPTY reflows it (P7). The scrollback is: rows that scrolled off carry no
join information, so f4 cannot re-wrap them.

Three ways around it follow, in order of elegance. The standalone probe has
answered step zero on 22000: conhost does not repaint scrollback (P16), a
4000-column resize is accepted and cheap (P15), and the frame is delimited by
cursor visibility plus an XTWINOPS size report (P14). The version-6 real-f4
run then found the implementation error (P21): ConPTY's viewport had been
compared with f4's differently aligned viewport instead of with f4's local
history journal. The implementation now uses that journal; a repeat on
24H2/25H2 is portability work, not missing design information for 22000.

**(a) The resize oracle — make ConPTY tell us the line structure.** P7 is a
gift: on any resize ConPTY repaints the viewport with every line **rejoined**.
So the line structure of the current viewport can be *asked for*: resize the
pseudoconsole to a very wide width (`COORD.X` is `int16`, so up to 32767; a
few thousand is enough), read the repaint into a shadow grid (not the display),
resize back, and read the second repaint. The wide frame has one row per
logical line; matching it against the narrow frame yields whether each row
continues into the next. The narrow repaint is aligned by exact consecutive
row pairs against `GridHistory + viewport`, not by screen coordinate. Only a
pair unique in both sides is allowed to stamp `GridHistoryWrap`/`WrapFlags`.
This both tolerates private rows excised by f4 and retroactively corrects rows
already outside f4's viewport. When those rows later leave the bounded grid
journal, the corrected boundary is encoded in the permanent log, so history
which ConPTY has since discarded still reflows correctly. Rules that make it
safe: only when the session is idle at a prompt with no console child, never
on the alternate screen, and parse oracle frames into a scratch view. Cost:
two hidden repaints per idle prompt, a few hundred bytes each. Boundaries which
scrolled beyond both ConPTY's overlap and the local oracle journal before any
pass retain the conservative hint from (b); the lost bit cannot be recreated
post hoc.

**(b) The `ESC[K` hint (winpty's and WezTerm's guess, done honestly).** P6: ConPTY emits
`ESC[K` after a row that ends short of the width and nothing after a row that
fills it. A full row followed by CRLF and no `ESC[K` is *probably* a wrap;
the ambiguity is a hard-break line that is exactly the width, once in W lines.
Use it only to stamp rows the oracle did not reach, and mark the stamp as
"guessed" so a later oracle pass can overrule it.

**(c) Own the wrapping: a very wide pseudoconsole, wrap in f4.** If the
pseudoconsole is always, say, 4000 columns wide, ConPTY never wraps, every
logical line arrives as one row, and f4 wraps at the real width with full
knowledge. Rejected, and recorded so nobody re-derives it: every program that
formats to the console width (`dir /w`, `more`, any TUI) would format to 4000
columns. The oracle (a) borrows the wide width only for a moment, when nothing
is running.

With (a)+(b), Windows gets what Unix has: rows in the history know whether
they were wrapped, `reflowLocked` can join them, and the width-only reflow
already shipped becomes correct on Windows too. The remaining honesty: the
oracle costs a repaint the user does not see, and ConPTY's own reflow of the
viewport must be *accepted* (parsed, not fought) rather than duplicated — on a
width change f4 should let ConPTY's repaint land and reflow only the history
above it, then stitch the two at the seam using the stamps.

### 3.3.1. Both hypotheses at once: a switch, so one tester run decides — **implemented**

The tester who can run this is not always available, and two hypotheses are
open at the same time. So the implementation ships **both** (a) and (b) behind
one selector and a diagnostic mode, and a single run of the build reports on
each. Nothing is guessed at in the field; the field just reads back.

Code: `cmd/f4/reflow_oracle.go` (modes, the oracle, the matcher),
`TerminalView.HintWrap` and `elBeforeBreak` in `terminal_view.go` (the hint),
`PanelsFrame.consumeLocalOutput` (the diversion of frames to the scratch
parser), `cmdShellSession.release` (the trigger). Tests:
`reflow_oracle_test.go`, driven by `fakeConPTY`, which reproduces P5–P7 from
the probe bytes — every mode is run through the probe's own scenario.

**The selector.** The safe oracle is now the Windows default. An environment
variable remains as a diagnostic/escape hatch and can be set per launch:

    F4_WIN_REFLOW=oracle    (a) resize oracle, then (b) for rows it did not reach
    F4_WIN_REFLOW=hint      (b) only: the ESC[K full-row guess
    F4_WIN_REFLOW=off       legacy Horizontal Preservation, no stamps
    F4_WIN_REFLOW=probe     diagnostic: hint on, oracle runs at every idle prompt
                            but stamps nothing; logs what it would have stamped
                            next to what hint did

Unset means `oracle` on Windows and retains the platform's existing reflow on
Unix. `off` is explicit and remains the escape hatch. Read once at startup
into `terminalReflowMode`; the Unix path still needs neither hint nor oracle.

**Why `probe` mode is the important one.** It answers the step-zero questions
from §3.3 without asking anyone to run a separate tool: for each oracle pass it
logs, under `--debug` with the prefix `REFLOW_ORACLE:`, (1) the row count of
the wide repaint versus the narrow one — if the wide frame ever has *more*
content rows than the viewport had, conhost keeps scrollback under ConPTY and
the oracle is recovering history; (2) whether the frame was delimited by
`ESC[?25l … ESC[?25h` (it keys on that), or the parser had to fall back to a
timeout to know the frame ended; (3) the stamps it computed, alongside the
stamps `hint` would have computed for the same rows, and where they disagree.
Point (3) is the head-to-head: over an ordinary session the log says how often
the guess is wrong, which is the number that decides whether `hint` alone is
acceptable on builds where the oracle is unavailable.

**What each mode must do.**

`oracle`:
- Only fires when `cmdShellSession` is idle at a settled prompt with no
  console child (it is the only component that knows this), never on the
  alternate screen, never while a resize is already in flight.
- Sequence: remember the real size; `ResizePseudoConsole(wide, H)`; route the
  incoming frame to a scratch `TerminalView` (a second parser instance bound to
  it — the display's parser sees nothing); `ResizePseudoConsole(real)`; route
  that frame to the scratch view too; then restore routing. The display is
  never touched: the second frame puts ConPTY's own state back exactly, and
  the display's grid was already equal to it.
- Match: the wide frame's rows are logical lines; walk the narrow frame's rows
  and consume the wide rows' text, stamping `ExplicitLineBreak` on the last
  significant cell of each narrow row where a wide row ends. Text must match
  exactly, padding aside (P6: full rows are padded with spaces); a mismatch
  aborts the pass with a `REFLOW_ORACLE: mismatch` log line and stamps nothing
  — never a partial stamp.
- Rows that scrolled off before the pass keep whatever `hint` gave them.
- Wide width: 4000. `COORD.X` is `int16`; 4000 leaves every realistic line
  whole and is far below the limit.

`hint`:
- In the parser's CRLF handling, when the row just finished is full-width and
  no `ESC[K` preceded the CRLF, stamp the row as *continued* (no
  `ExplicitLineBreak`); otherwise stamp the break. The stamp is marked
  guessed (`ImportantLineChar` on the same cell is free for this — far2l
  uses it for a related purpose, and it is already declared).
- The oracle overrules a guessed stamp; it never overrules a certain one.

`off`:
- Exactly today. This is the control group for the tester.

**Two things the implementation settled that the design left open.** The
oracle never writes a partial result: a narrow frame that differs from the
display, or two frames that do not describe the same text, abort the pass with
a `mismatch` line and stamp nothing. And the pass waits for the stream to be
quiet (`oracleQuietBefore`) before its first resize, so the diversion never
swallows ordinary output; the display's cursor and rows are compared before
and after, and a change there also aborts.

**What the tester reports, per mode, in one message:**

| Mode | Do | Report |
| --- | --- | --- |
| `off` | `ls -la`-style wide output, narrow the window, widen it | the control: what today looks like |
| `hint` | same | are long lines re-joined on widen; any line wrongly glued |
| `oracle` | same, plus leave the shell idle and watch the screen | are long lines re-joined; **any flicker or cursor jump** at the prompt (that is the oracle firing and would be a bug) |
| `probe` | ordinary use for a few minutes with `--debug`, attach the log | the `REFLOW_ORACLE:` lines |

If `oracle` shows any visible effect at all, it is wrong — the frames are
meant to be invisible — and the log from `probe` says which assumption broke.

### 3.4. Cell-level wrap marker (prerequisite for 3.3)

`WrapFlags[y]` is a row array shifted by hand in `scrollUp`, `scrollDown`,
`EraseLine`, `EraseDisplay`, `Resize` and the history push. The oracle stamps
cells; so should everything else. Move the marker to `ExplicitLineBreak` on the
last significant cell (far2l's polarity: default is *continuation*, the mark
says *this line ended here*). The three dozen touch sites shrink to none, and
the marker travels with the character through every relayout. Schedule it away
from Windows field testing: the code is shared and runs on every character.
