# ConPTY and line structure: the research, the algorithm it justifies, and how it held up

This is the one-document version of a question that took two Windows builds,
three probes, eight field runs and nine documents to answer: **how does a
terminal running inside a ConPTY tell a line that wrapped from a line that
ended, when ConPTY does not say?** Every step below states what was asked,
how it was asked, what the answer was, and how that answer is now pinned in
the test mocks so the code cannot drift from it silently. The last section
is the part that matters for anyone who inherits this: which of these facts
the reflow depends on, how it fails if one of them changes, and the switch
that turns it off.

The detailed ledgers this summarizes: `TERMINAL_LEDGER.md` (every finding,
numbered), `TERMINAL_CONPTY_FINDINGS.md` (the raw evidence and the chase),
`TERMINAL_REFLOW.md` (the algorithm as implemented), `WINCON.md` and
`WINCON_805_HANDOVER.md` (the picture overlay, which turned out to share a
root cause). Finding numbers (P6, A5, W8, ...) refer to the ledger.

## 0. The problem

f4 hosts `cmd.exe` in a pseudoconsole and renders its output itself. When the
window is resized, long lines should re-wrap the way they do in any modern
terminal. On Unix that is bookkeeping: the pty delivers bytes, the terminal
wraps them, and the terminal knows which of its rows are continuations. On
Windows the pseudoconsole (ConPTY) sits between the shell and f4 and *renders
the screen itself* into a VT stream, so what f4 receives is not the shell's
output but a description of a screen -- rows, cursor moves, erases -- and the
one thing that description does not carry is whether a row is the end of a
line or the middle of one.

The user-visible failure was simple: resize the window and history either
stays wrapped at the old width or vanishes.

## 1. What ConPTY does (measured, two builds)

Each step was a scenario in `tools/conptyprobe` (a Windows binary that
creates its own pseudoconsole, drives `cmd.exe` inside it, and records every
byte), run by the maintainer on 10.0.19045 and 10.0.22000.2538. The probe's
log is the evidence; the finding is what the log proved.

| Step | What we checked | How | What we found | Pinned in the mock as |
|---|---|---|---|---|
| 1 | Does ConPTY mark a soft wrap at all? | Print a line longer than the width; record the live stream. | No. On 19045 the wrap point carries a **hard CRLF** (P6). Full rows are padded to width; a short row is followed by `ESC[K`, a full one is not. | `fakeConPTY.print`: full rows padded, CRLF, `ESC[K` only after a short row. `TestFakeConPTYLiveStreamBreaksWithHardCRLF`. |
| 2 | Is that universal? | Same on 22000; read the bytes with a cursor model, not a CRLF split. | No. Within one build the live stream **sometimes has no terminator at all**: the line is written whole across the boundary, the terminal's autowrap breaks it, and ConPTY repositions with an absolute CUP afterwards (P11). A CRLF-only reading saw a 140-column row. | `fakeConPTY.printUnterminated`. `TestFakeConPTYUnterminatedLiveStream`, and `TestHintReadsBothLiveShapesAlike` proves the hint gives the same answer on both shapes. |
| 3 | What does a resize repaint look like? | `ResizePseudoConsole`, drain the frame. | A full-viewport repaint bracketed by `ESC[?25l` ... `ESC[?25h` (P7). On 19045 wrapped lines are written as rows joined by CRLF; on 22000 the logical line is written **whole** and autowrap places it, only the last row ends in CRLF (P12). On 22000 the frame opens with the XTWINOPS size report `ESC[8;rows;cols t` (P14). | `conptyBehaviour` per build: `repaintBreaksWrappedLines`, `sizeReport`. `TestFakeConPTYRepaintShapeMatchesTheBuild`, `TestFakeConPTYRepaintsOnEveryResizeCall`. |
| 4 | Is the `ESC[K` clause reliable? | Print a hard-broken line *exactly* the width. | It arrives as a full row + CRLF with no `ESC[K` -- indistinguishable from a wrap (P13). The hint is wrong once in W lines, and only in that direction (a hard break read as a wrap). | `TestFakeConPTYExactWidthLineIsAmbiguous`. |
| 5 | Does ConPTY keep scrollback we could ask for? | 30 lines into a 12-row console, then widen; look for scrolled-off lines in the repaint. | **No** (P16). The repaint covers the viewport and nothing above it. Whatever scrolled off is gone from ConPTY's side; only f4 has it. | `fakeConPTY.trimToHeightLocked`. `TestFakeConPTYKeepsNoScrollback`. |
| 6 | Do the documented flags change any of this? | `RESIZE_QUIRK` (0x2) and passthrough (0x8) vs 0, byte-for-byte diff. | Accepted, no effect on either build (P8-P10). | Not modelled: nothing in f4 depends on them. |
| 7 | Can we *make* ConPTY tell us the line structure? | Widen the pseudoconsole to a very large width for one frame; read the repaint. | Yes: the wide repaint carries every wrapped line **rejoined** (P15), and the narrow frame after restoring the width shows where each line breaks. Two frames, one answer per row. This is the oracle. | `TestFakeConPTYWideResizeRejoins`; the oracle tests in `reflow_oracle_test.go` run the whole exchange against the fake. |
| 8 | When is it safe to borrow the console for that? | Watch the title ConPTY forwards. | `cmd.exe` sets the title to `... cmd.exe - <command>` while a command runs and drops the suffix when idle (P18, P19). That is the "no child is running" gate the oracle waits for. | `fakeConPTY.title`. `TestFakeConPTYTitleBusySignal`. |
| 9 | Which resizes repaint, and how does a repaint differ from output? | Height-only resize; resize to the size ConPTY already has; compare the bytes with a command batch. | **Every** `ResizePseudoConsole` call repaints (6.15), including a call for an identical size (6.16) -- and that one carries **no** size report. Both were guessed wrong first and cost the field runs of §3. | `fakeConPTY.SetSize` repaints on every call; same size ⇒ no `ESC[8;`; a repaint goes to home after the hide, a command batch (`printBatch`) positions below it. `TestFakeConPTYRepaintsOnEveryResizeCall`, `TestAbsorbNeverTakesCommandOutput`. The probe measures all three (`repaint.*.frame/size_report/starts_at_home`). |

| 10 | Can a repaint be told from a program redrawing its own screen? | Compare a resize repaint with `cls`, and with a full-screen program (f4 inside f4) taking the alternate screen. | **Not by looking at it.** Both open with the hide and go home; the shape is necessary and not sufficient. What separates them is context f4 already has: ConPTY owes exactly one repaint per `ResizePseudoConsole` call, and a full-screen program is on the alternate screen, where f4 does not re-wrap at all (6.19). | `TestNestedFullScreenProgramKeepsItsFrames`, `TestClsStyleRepaintIsNotDroppedWithoutAResize`. |

The line that connects all ten: **ConPTY describes a screen, not a
document**. It will repaint the screen it holds, at whatever size it is asked,
faithfully -- and it holds nothing else. Every hard part of this work followed
from that one sentence: the line structure has to be inferred or provoked,
the history has to be f4's own, and a repaint that arrives has to be judged
by what f4 asked for rather than by what it looks like.

## 2. The algorithm, and which fact each part rests on

Given the above, the design has three parts, in the order they were adopted.

**The hint (rests on steps 1, 2, 4).** While reading the live stream, a row
that is full to the width and is *not* followed by `ESC[K` is marked as a
wrap; the terminal's own autowrap marks the no-terminator shape the same way.
This is cheap, runs on every row, and is wrong once per W lines in one known
direction (P13). It is what carries the history in practice (W8, O12).

**The oracle (rests on steps 3, 5, 7, 8).** At a settled prompt with no
command running, borrow the console: resize it very wide, read the rejoined
repaint, restore, read the narrow one, and stamp wrap flags into
`GridHistory` only for rows that align exactly with both frames and the
local journal. Every stamp is checked before it can change history, and a
pass that cannot prove the two frames describe the same text stamps nothing
("display changed during the pass"). Measured on 22000: 25 boundaries in one
pass, none stale, and **zero disagreements with the hint** wherever it could
check (W8).

**Ownership of the viewport during a resize (rests on steps 3, 5 and 9).**
This is the part that was missing, and the reason the feature looked broken
for seven field runs after it worked. f4 re-wraps its own grid from history
when the width changes; ConPTY then repaints its screen -- which has no
history -- and that repaint used to land on top, replacing recovered rows
with blanks. Two rules fix it: tell ConPTY nothing when the size did not
change (so it sends nothing), and drop ConPTY's resize repaint, recognised by
its shape and not by its timing: the cursor hide, then the size report where
the build sends one, then the move to **home** -- a repaint redraws from the
top, a batch of command output positions below home. Recognising it by
timing, or by the cursor hide alone, took real output during a `dir` and lost
it (6.18): ConPTY hides the cursor around every batch it writes. The shape
rule cannot take output (`TestResizeDuringCommandDoesNotEatOutput`,
`TestLongScrollingDirWithResizesLosesNothing`) and takes a late or split
repaint just the same (`TestLateResizeRepaintIsStillAbsorbed`).

A fourth fact turned out to be load-bearing for the history itself, not for
ConPTY: the history must be bounded in **logical lines**, not rows, because
the same text is more rows at a narrower width, and a row cap evicts on every
narrowing step (6.11, 6.12; `TestGridHistoryBoundIsWidthIndependent`).

## 3. What the field runs taught, in the order they happened

This is recorded because the *method* cost more than the bug, and the next
person to chase something similar should not repeat it.

1. The oracle was reported not to work. It worked; the runner's verdict
   treated a safely-rejected pass as a failure (6.6).
2. The scrollback was reported not to come back. The run was in `probe`
   mode, which re-wraps nothing by design, and the log did not say so. The
   mode line now names every switch it sets (6.7).
3. In the default mode, the log showed 197 repaint frames landing after f4's
   re-wrap. First cause found: the repaint overwrites (6.8). Absorbing it
   changed nothing on screen.
4. Instrumentation inside the re-wrap, one number per run, for four runs.
   Two of the counters could not observe their own subject and returned
   zeros that read as evidence (6.14). The re-wrap was innocent throughout:
   `TestReflowLosesNothingAcrossEveryResizeShape` now says so in one second.
5. The history cap was found and fixed (6.11, 6.12). Still no change on
   screen.
6. Logging *around* the re-wrap, all at once: which view, what ConPTY was
   told, what it sent back with its declared size checked, what was drawn.
   The next log showed characters preserved inside every pass and lost only
   between passes (6.15).
7. Cause two: height-only steps let the frame through (absorber gated on
   width). Fixed. Still no change.
8. Cause three, twelve lines of log: three resize events for a size the view
   already had, each still calling `ResizePseudoConsole`, each answered by a
   repaint **without a size report** that nothing recognised (6.16). Fixed.
   Confirmed working.
9. Careful testing of that build found two more: a late repaint landing
   (only a few rows shown until the next resize) and -- the one that matters
   -- a resize during `dir` eating the listing. Both were the absorber keyed
   on timing and on the cursor hide, which every ConPTY batch carries. Both
   were reproduced on the mocks *before* the fix, and the fix recognises a
   repaint by its shape (6.18).
10. That shape rule was necessary and not sufficient: a program clearing the
   screen and repainting from home matches it, and one such program is f4
   inside f4's own terminal. Two conditions fixed it -- a repaint is dropped
   only when ConPTY owes one, and never on the alternate screen (6.19).
   Written after a reviewer pushed back on the claim that full-screen
   programs were out of scope. They are not, and the claim was wrong.
11. A review before the next field run, reading the code rather than the
   notes about it, found four ways the absorber could still lose bytes --
   a repaint coalesced with output in one read, a frame with no close, a
   debt raised without a call, a clamp too low for a slow ConPTY. All four
   got a failing test first, then a fix (6.21). None had reached the field.

Two things would have shortened this to one run: asking how the symptom was
reproduced (a corner drag interleaves width, height and same-size steps, so
every hypothesis was tested against a log where the innocent path ran
constantly); and the end-to-end test that now exists,
`TestCornerDragKeepsTheScrollback`, which drives the real resize path against
a fake that repaints on every call, on both builds.

## 4. How robust is this, and what is the fallback

**What the reflow assumes about ConPTY**, and what happens if each assumption
breaks on some build:

| Assumption | If it changes | Consequence | Detected by |
|---|---|---|---|
| A resize repaint opens with the cursor hide, [size report], then home (P7, P14, 6.18). | A build repaints from somewhere other than home, or stops hiding the cursor. | The absorber takes nothing; the frame lands after f4's re-wrap and overwrites it -- the 6.8 symptom, visible, not destructive. It can never take output: nothing that is not a repaint matches the shape. | `REFLOW_FRAME ... diverted=false` in the log; `repaint.*.starts_at_home=no` in the probe. |
| A full row without `ESC[K` is a wrap (P6). | A build starts erasing after full rows too. | The hint marks nothing; history re-wraps as if every row ended a line. Wrong shape, no loss. | `REFLOW_ORACLE ... where hint and oracle disagree` becomes nonzero. |
| The wide repaint rejoins lines (P15). | A build clamps the width or keeps wrapping. | The oracle aligns nothing and stamps nothing -- by design it cannot stamp what it cannot prove. The hint carries on alone. | `REFLOW_ORACLE ... nothing stamped` on every pass. |
| The title carries the busy suffix (P18). | A shell or locale without it. | The oracle never finds a safe moment and never runs. Hint only. | No `REFLOW_ORACLE` lines at all. |
| ConPTY keeps no scrollback (P16). | A build starts keeping it. | Harmless: f4 keeps its own and ignores what the repaint has above the viewport. | -- |
| Exactly one repaint per `ResizePseudoConsole` call (6.19). | A build answers a resize with silence, or with two frames. | Silence: the owed count lingers and one later home-repaint is misread -- one visible frame, clamped so it cannot accumulate. Two frames: the second lands after f4's re-wrap and overwrites it, the 6.8 symptom. | `REFLOW_SUMMARY ... repaints owed` staying above zero. |

The pattern is deliberate: every part of the reflow fails *toward the hint*,
and the hint fails toward "no re-wrap", never toward lost text. Content loss
requires something outside these assumptions -- which is exactly what
happened, and why the `chars` figure in `REFLOW_WRAP` exists.

**Backward compatibility.** Microsoft's history with ConPTY is the relevant
prior: the 19045 → 22000 change (P12, CRLF-joined rows becoming
autowrap-placed whole lines) did not break the design because the design
reads a cursor model rather than terminators. The size report (P14) appeared
between builds and the reflow does not depend on it. The behaviours it does
depend on -- the frame brackets, `ESC[K` after a short row, no scrollback --
are how ConPTY has rendered since 1809 and are what Windows Terminal itself
relies on. A change there would be visible in every terminal on Windows, not
only in f4.

**Earlier and later builds.** Nothing below 19045 has been measured. ConPTY
exists from 1809 and its renderer is the same code lineage, but "same
lineage" is a hope, not a finding. 24H2/25H2 are equally unmeasured. For both,
the probe is the instrument and the ledger's O4 is the open item; the
`conptyBehaviour` table in the mock is where a third build's answers go.

**What a user's log shows without any of this.** Three lines, budgeted so a
drag costs a handful rather than hundreds: `REFLOW:` at startup names the mode
and every switch it set; `REFLOW_ABSORB:` reports the first few repaints of a
burst and every fiftieth after, with the resize and owed counts;
`REFLOW_SUMMARY:` on shutdown and every fiftieth child resize gives mode,
resizes, repaints absorbed and owed, oracle passes, and the history's rows and
characters. Between them they answer, from a `--debug` log alone, every
question each field round trip in §3 was spent on: whether the feature was on,
whether ConPTY was resized, whether its repaints were recognised, whether the
oracle ever ran, and whether the history is still there (6.20).

**The conservative switch.** `[Terminal] WindowsReflow = off` in the config
(or `F4_WIN_REFLOW=off` in the environment, which wins) returns the Windows
terminal to Horizontal Preservation: no hint, no oracle, no absorber, the
resize repaint applied as ConPTY sends it. That asks nothing of ConPTY beyond
what every build has done, and is the right answer on a build where the
`REFLOW_*` log lines show any of the assumptions above failing. `hint` is the
middle setting: no oracle passes, no console borrowed, wrap guesses only.

## 5. The same root cause, next door

The picture overlay for classic conhost (#805) was investigated in parallel
and turned out to fail for a reason of the same shape: something f4 did not
own was repainting or freezing on top of its output. There it was the console
window's input queue, coupled to f4's by a cross-process child window (F7,
measured in the field as a frozen console, F22), and the fix was the same
kind of fix -- stop depending on the other process: a top-level layered
window with no parent and no owner, only ever *read* from the console (F23).
Under Windows Terminal the picture was never being erased at all; the
terminal was simply not being asked whether it could draw (F13, F14; fixed by
reading the window class and DA1). `WINCON.md` has the full account.

## 6. What is still open

- Portability to builds other than 19045 and 22000 (O4). The probes in the
  issue threads are current: `f4probe.zip` (this document's section 1,
  automated, now including the same-size and height-only resize steps) and
  `f4imgprobe.zip` (the overlay and sixel questions, eight answers from a
  person).
- Reading the XTWINOPS size report to tie a late frame to its resize (O9).
  Two `STALE` frames were seen in one run; with the absorber covering every
  resize they no longer reach the display, but they say the size ConPTY lays
  out for can lag f4's by a step.
- The tracker of the new overlay under minimize, occlusion and foreground
  changes has not been exercised on a live f4 (Q6, Q7).
- One deliberate misreading remains, recorded rather than fixed: a `cls`
  issued in the same breath as a resize can have its repaint counted as the
  one ConPTY owed. It costs one visible, recoverable frame and never output.
- Whether a build exists whose resize repaint does not start at home. The
  probe now records `repaint.*.starts_at_home` precisely so this can be
  answered without another round trip.


## 7. Verdict: abandoned. Do not come back to this.

After eleven field runs the Windows reflow -- the `ESC[K` hint, the
wide-resize oracle, and the repaint absorber in all three of its forms -- is
removed from the codebase. This section is written so that nobody, including
the author of the next clever idea, rebuilds it.

**What the last two runs showed.** With every fix of §3 applied, a resize
during a `dir` still corrupted the Terminal Log: duplicated rows, tails of
lines placed at the column where ConPTY's buffer had them, blank stretches.
The stream explained it (6.22): ConPTY's output after a repaint is a delta
against that repaint. Gating the absorber on "idle" moved the failure rather
than removing it, and the run after that (duplicated rows, corrupted data)
was the proof. Every fix in this file made the symptom rarer; none made it
impossible, because none could.

**Why it cannot work.** The design put two owners on the same rows. ConPTY
owns its viewport and re-renders it, from a buffer that holds nothing above
the screen (P16), sending only deltas against the last frame it believes the
terminal displayed (6.22). f4 owned a re-wrapped history and a viewport
composed from it. Where the two met -- the seam -- there is no identity for a
row: nothing in the stream says "this row is that row", so every join was a
guess (the hint, the oracle's alignment, the absorber's shape rule), and each
guess was right until the next resize arrived while something was in flight.
A mechanism that is correct only when nothing is happening is not a
mechanism.

**What every other terminal on Windows does.** WezTerm, Alacritty and Windows
Terminal stand *outside* ConPTY: they are the terminal, ConPTY is the
renderer, and they display the frame it sends. The viewport reflows because
ConPTY reflows it. Their scrollback is kept as written, and the duplicated
rows after a resize that this project fought are a known, accepted ConPTY
limitation there too. f4 was the only program trying to be an application
inside ConPTY *and* a terminal with its own re-wrapped history at once.
Nobody else is in that position because it is not a position.

**What remains, deliberately.** ConPTY owns the viewport; on a resize its
repaint is applied as sent (Horizontal Preservation, the behaviour before
this work). A wrap flag is set only by the terminal's own autowrap, never
guessed from the stream. The history is bounded in logical lines rather than
rows (6.12) -- that was a real bug, independent of all this, and stays fixed.
The probes, the fake ConPTY and the ten measured findings of §1 stay as
documentation of what ConPTY does; they are the reason this section can be
written with confidence instead of regret.

**If the scrollback under Windows must ever re-wrap**, the only honest routes
are outside this design: an upstream ConPTY that re-renders more than the
viewport, or a row identity in the stream that ConPTY does not provide today.
Not a fourth condition on an absorber.

## 8. Roads not taken: the alternatives to §7, assessed

Written in the last minutes of the session that closed §7, deliberately
before any code, because §3 shows what happens when code comes first.

**A. Bypass ConPTY for programs that do not need a console.** Rejected too
fast the first time. `cmd`, `dir`, batch files and anything on the Win32
console API need ConPTY. But WSL programs, PowerShell 7 with VT output, and
the Go/Rust/Python utilities people actually run write bytes to stdout and
have never heard of a Windows console. Started with **pipes** instead of a
pseudoconsole, they hand f4 a plain VT stream, and f4 is their terminal the
way xterm is `ls`'s: the wrap is f4's own, the history is f4's own, and
there is no frame, no delta and no seam -- the whole of §7 does not apply.
Two real problems, both tractable: without a console `isatty` is false, so
colour and paging need `TERM`/`COLORTERM`/`FORCE_COLOR`, and WSL programs
should be launched through `wsl.exe` where the Linux side gives them a real
pty anyway; and the class of a program cannot be known in advance, only
chosen by kind -- `wsl.exe`, PowerShell 7+, known VT tools by pipe; `cmd`,
`.bat`, anything else by ConPTY. A shell that later spawns a VT program stays
on ConPTY, and there reflow is what it is for everyone. **The most promising
road, and cheap to try for `wsl.exe` alone.**

**B. A console scraper of f4's own, instead of ConPTY.** What winpty was:
read the buffer of a hidden console with `ReadConsoleOutput`, diff, render.
It gives full control of synchronisation -- no deltas against a frame f4 did
not show -- at the price of seeing only the buffer (no scrollback, but f4 is
the scrollback), flattening the attributes of VT programs, missing alternate
screen transitions, and polling. Real, and months of work, not a session.

**C. A window of height zero: everything goes straight to history, render
the last rows cut to the real width.** ConPTY cannot be that (minimum height
one, and it repaints a buffer rather than emitting lines). But combined with
B, or with ConPTY kept **very wide** (4000 columns, as the oracle did) and as
tall as the window, every logical line arrives whole and ConPTY never wraps
at all; f4 cuts to the window width itself, and the wrap question disappears
because nobody but f4 ever answers it. Full-screen programs -- f4 inside f4,
editors -- need a console of the real size, so they need detecting (the
alternate screen, or the child's console mode) and a real-size console when
they run. **Cheap to measure with one probe run before any code: does a
4000-wide ConPTY of window height deliver lines whole and repaint sanely.**

**C, measured (2026-08-28).** `tools/conptycprobe` ran on Windows
10.0.22000.2538 with an outer terminal of 120x30 and a 4000x30 ConPTY. It
emitted two ASCII lines of 184 and 3968 characters from an `@echo off` batch,
then resized only the height through 4000x29, 4000x30, 4000x31, and 4000x30.
Every initial and repaint check reported `whole=true`, `split=false`,
`rows=1`; a post-resize line did too, and the probe returned `PASS`.

This confirms the premise of C on this build: a very wide ConPTY can carry
these lines without answering the horizontal-wrap question, and its
height-only repaint remains coherent. It does not yet test f4's wide-console
integration, rendering/cutting to the real width, scrollback ownership,
alternate-screen or full-screen programs, width changes, or another Windows
build. The first run was a false negative caused by interactive command echo;
the probe was corrected to run the payload from an `@echo off` temporary batch
before this PASS.

**C, width-aware command follow-up (2026-08-28).** The Linux companion probe
was run in `/dev/pts/2`, with an outer size of 153x36, and compared real PTYs
of 80, 120, and 4000 columns. The result separates two classes that must not
be conflated. `ls -1` stayed at 142 one-entry-per-line records at every
width, and `git branch --column=never` stayed at 41 records. Human-oriented
column modes did react strongly: `ls -C` produced 142 lines at 80 columns,
71 at 120/153, and **one line of 3658 characters at 4000**; Git's
`branch --column=always` produced 21, 14/11, and **one line of 1439
characters**, respectively. The small `git diff --stat` fixture stayed at
two short lines at all widths, so it did not exercise a width decision.

This makes the practical risk real but bounded: a 4000-column ConPTY does
not damage ordinary newline-delimited output, but it materially changes
common human-facing listings and tables. It also disproves using a write to
the very last cell as the only detector: in this run the width-aware commands
made decisions based on 4000 without reaching column 4000 (`ls -C` stopped at
3658 and Git at 1439). The saved log was complete and ended with `END`; the
earlier apparent hang was a test-runner/pager issue, not a ConPTY result.

The Windows command probe has an independent pagination guard. It records and
then clears `DIRCMD`, invokes `dir` with `/-p`, and starts PowerShell with
`-NoProfile -NonInteractive`. Thus a user's persistent `/P` setting cannot
turn the measurement into a keypress wait. The tested PowerShell formatting
cmdlets do not request `Out-Host -Paging` and have no pager by default.

**D. Make the Terminal Log the answer.** It already holds logical lines, so
reflow there is free, and it may be all users need from history under
Windows. The honest minimum, and the fallback if A and C do not pay off.

Order to try: C's probe is now a PASS on 10.0.22000.2538; its integration and
portability still need a prototype and newer-build measurements. Then try A
for `wsl.exe` (the case where it is nearly free), and decide whether B is
worth its months.
