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

The Windows command probe records and then clears `DIRCMD`, invokes `dir` with
`/-p`, and starts PowerShell with `-NoProfile -NonInteractive`. The follow-up
run below showed that this is not sufficient to make a large native `dir`
listing non-interactive, so the probe also bounds its native fixture. The
tested PowerShell formatting cmdlets do not request `Out-Host -Paging` and
have no pager by default.

**C, Windows command follow-up (2026-08-28).** The Windows run was on Windows
11 Pro `10.0.22000.2538`, PowerShell `5.1.22000.2538`, with an unredirected
`120x30` window and a `120x9001` screen buffer. `TERM`, `COLUMNS`, and
`LINES` were empty, and Git was not installed. The log therefore confirms a
real Windows console run, but does not identify the outer host as ConPTY;
that distinction still needs a run from inside f4.

The 142-entry fixture exposed a second harness problem. `dir /w`, `dir /d`,
and `dir /b` all emitted repeated `Press any key to continue . . .` prompts at
screen boundaries, even though the recorded `DIRCMD` was empty and the probe
used `/-p`. They eventually returned zero, but they were not safe as
unattended measurements. The Windows probe now keeps the 142-entry fixture
for PowerShell and uses a separate, height-bounded `dir` fixture (10 entries
in this 30-row run), so the native commands complete without a keypress wait.

PowerShell's `Format-Wide`, `Format-Table`, `Get-Process | Format-Table`, and
default `Out-String` all completed without paging. The table output did
truncate the long `FullName` column to the available width, confirming that
PowerShell formatting is width-aware. The Russian filename appeared as
mojibake in the transcript under OEM code page 850; that is a separate log
encoding issue, not a width result.

**D. Own the console server: build conhost into f4.** The one road that
attacks the root rather than working around it.

Every other option in this file gropes for the wrap flag from outside.
Nothing gropes for it because it is missing -- it *exists*, and has since the
first console: `TEXT_BUFFER`'s rows carry `wrapForced`, set when a write ran
off the right edge, and `TextBuffer::Reflow` already re-wraps a whole buffer
using it. That is exactly the fact §7 says the stream cannot supply. **It is
not exported by any public console API** -- not `ReadConsoleOutput`, not
`GetConsoleScreenBufferInfoEx`, not the VT stream ConPTY emits. Only the
process that owns the buffer can read it. That is the real reason winpty,
WezTerm and this project all ended up guessing: everyone is outside the one
process that knows.

So be that process. Two facts make it plausible rather than a rewrite of
Windows:

- **conhost is open source.** `microsoft/terminal`, `src/host`, MIT. The
  buffer, the wrap flag and the reflow are all there, written and debugged by
  the people who own the format.
- **Windows already supports substituting the console server.** Since 1809
  conhost takes `--server <handle>`; ConPTY starts its own conhost exactly
  that way. The mechanism f4 would use is the one Microsoft uses, not a hack
  around it.

What f4 would gain: the wrap flag as a fact rather than an inference, and a
reflow it does not have to invent. What it costs: a C++ component in a Go
program (cgo), building `src/host` outside its own solution, three
architectures, and ownership of a console server's correctness and security.
Comparable in size to B, and strictly better in kind -- B still reads a
buffer someone else already wrapped.

ReactOS reached the same place from the other side, reimplementing `condrv`
and the CSRSS side of the protocol. That is the fallback shape if the
supported route closes, and it is genuinely a rewrite; the `--server` route
is not.

**The one-evening question to answer before any of this**: does `src/host`
build standalone, outside the `microsoft/terminal` solution, and does the
resulting binary serve a console when launched with `--server`? If yes, the
road is engineering. If no, what remains is the ReactOS-scale fork, and that
is a different decision.

Note what this does *not* fix, so nobody is surprised later: text that
arrives already wrapped by someone else -- a remote host over ssh, where the
far side's pty made the layout decision -- stays wrapped as received. Owning
the local console server gives f4 the wrap flag for locally produced output.
Nothing gives it the flag for a layout that was decided on another machine.

**D2. Proxy the console server instead of replacing it.** D says own the
server; this says stand in front of it. The seat is the same handle --
`\Device\ConDrv\Server` -- but f4 need not implement a console: it can hold
the endpoint, forward the traffic to the real conhost, and read what goes
past. **No C++ in the build, no console server of our own to get right.**

What goes past is better than what D would read, not worse. The wrap flag
is conhost's *conclusion*; the messages carry the application's *intent* --
"client 4 called `WriteConsoleW` with these 185 characters" -- and the
buffer width at that moment is known. One logical line over two rows is then
a fact, not an inference. Everything the project has tried so far looked at
the screen *after* the decision: the `ESC[K` hint read a rendered frame
backwards, a scraper (B) reads a buffer someone already wrapped, and the
oracle provoked a second frame to compare. Here the decision has not
happened yet.

It also supplies the detector that measurement C could not build: the
messages include `GetConsoleScreenBufferInfo`, so f4 sees *when a program
asks for the width* -- exactly the width-aware programs (`ls -C`, Git's
column mode, PowerShell's `Format-Table`) that made C unworkable, and which
could not be recognised by watching for a write to the last column.

**How stable is what this depends on?** Asked concretely, because "they
might change it" is not a risk assessment.

- The console *architecture* has changed three times in thirty years:
  CSRSS-hosted (NT 3.1 through XP), conhost over ALPC (Windows 7, 2009),
  and conhost over the `condrv.sys` driver (Windows 8.1, 2013). That is
  roughly once a decade, at the granularity of a Windows release family, not
  a monthly update.
- Within the ConDrv era it has been stable for **thirteen years**. The IOCTL
  set FireEye documented from a Windows 10 driver in 2017 is the same set
  named in `dep/Console/condrv.h` in `microsoft/terminal` today:
  `READ_IO`, `COMPLETE_IO`, `READ_INPUT`, `WRITE_OUTPUT`, `ISSUE_USER_IO`,
  `DISCONNECT_PIPE`, `SET_SERVER_INFORMATION`, `GET_SERVER_PID`.
- `conhost --server <handle>` has existed since 1809 (2018) and is how
  ConPTY starts conhost to this day. Microsoft depends on it in shipping
  code, which is the strongest guarantee available for something
  undocumented.
- The headers are **in the open-source repository**, vendored under
  `dep/Console` (`condrv.h`, `conmsgl1.h`, `conmsgl2.h`, `conmsgl3.h`), MIT.
  They are not *documented* -- microsoft/terminal#10463 asked for that in
  2021 and it is still open -- but they are published and they are what the
  shipping conhost is built against.

So the honest reading: the interface is undocumented but not volatile. The
risk is not a change every release; it is that when a change does come, no
compatibility promise applies and nothing announces it. That is a real risk
and it is bounded by one thing -- a proxy that fails should fall back to
plain ConPTY, not break the terminal.

**The measurement, and the probe that takes it.** `tools/condrvprobe` (a
Windows binary, no admin, changes nothing) answers three questions and
records the build, `condrv.sys` version and `conhost.exe` version they are
answers about:

1. Can an ordinary program create `\Device\ConDrv\Server`? If not, D2 is
   closed before it starts.
2. Does the driver deliver API messages, and what are their first bytes? The
   layout is undocumented, so the bytes are *recorded* rather than
   interpreted -- the only evidence of stability is the same bytes on
   another build.
3. Does the system conhost accept a handle f4 created, launched the way
   ConPTY launches it? If yes, the seat is real and the rest is forwarding.

**D2, first measurement (2026-08-28, Windows 10.0.22000, `condrv.sys`
6.2.22000.71, `conhost.exe` 6.2.22000.2538).** Two of the three questions
answered, and the two that matter answered yes.

- **The seat is available unprivileged.** `\Device\ConDrv\Server` was
  created by an ordinary user process. The obstacle that would have closed
  D2 before it began is not there.
- **The system conhost accepted a handle f4 created.** Launched as
  `conhost.exe --server <our handle> --headless -- cmd.exe`, it took the
  endpoint and kept running on it. So f4 can hold the seat and hand the work
  to the real conhost: no C++ console server of its own.
- **Question 2 failed on a probe bug, not a refusal.** `READ_IO` returned
  `ERROR_INVALID_FUNCTION` because the probe built its control codes with
  `FILE_DEVICE_CONSOLE = 0x8000` instead of `0x50`. The arithmetic is
  checkable against published numbers -- FireEye's 2017 analysis names
  `0x50000F` and `0x500013` for input-read and output-write, which are
  functions 3 and 4 under device `0x50` -- so the codes are now
  `READ_IO = 0x500006`, `SET_SERVER_INFORMATION = 0x50001F`. The probe also
  did not announce itself as a server first: `SET_SERVER_INFORMATION` hands
  the driver the event it signals, and without it a read has no meaning.
  Both are fixed; question 2 is unanswered, not answered no.

**What the run already settles about the shape of D2.** Questions 2 and 3
cannot both succeed in one process. If conhost serves the endpoint, conhost
reads the messages; if f4 reads them, conhost is not there to do the work. So
D2 is not "listen alongside" -- it is a genuine proxy: f4 holds one endpoint
facing the client, holds a second facing conhost, and forwards messages and
replies between them, reading what passes. That is more work than watching,
and it must be written down as such before anyone plans on the cheap version.

**A side note worth keeping.** `condrv.sys` reports file version
**6.2**.22000.71 -- a Windows 8 era resource, unchanged through Windows 11.
Weak evidence, but it points the same way as the interface history above:
this driver is not being rewritten.

**D2, second measurement (2026-08-28, same machine).** With the control
codes fixed, the probe got further and turned one of its own failures into
the most useful result so far.

- **f4 can take the server role.** `SET_SERVER_INFORMATION` was accepted: the
  driver took the probe's event. Holding the endpoint and *being* its server
  are different things, and both are now confirmed.
- **The server role is exclusive, measured rather than argued.** On the first
  run conhost accepted our handle and ran on it. On the second it refused
  with `0x80070016` (`ERROR_BAD_COMMAND`). The only difference between the
  runs was that the probe had claimed the server role in between. So an
  endpoint has exactly one server, first claimant wins -- which settles the
  shape of D2 for good: **not "listen alongside", but a real proxy** with two
  endpoints, f4 facing the client and conhost facing f4, messages and replies
  forwarded between them.
- **Question 2 is still open, and again on a probe fault.** A plain
  `CreateProcess` gives the child *the parent's* console, so `cmd.exe` went
  to the probe's own console and nothing arrived at the new endpoint; the
  three-second silence proved nothing. A client lands on a particular server
  only if handed that server's console handles, which are opened as child
  objects of the server handle -- `\Reference`, `\Input`, `\Output` -- and
  passed as its standard handles. The probe now does that, reports the
  NTSTATUS of each open so a failure names itself, and asks the driver
  `GET_SERVER_PID` afterwards to say whether anything actually attached.
  Question 3 now runs on a fresh, unclaimed endpoint, since the old one
  cannot answer it once we are its server.

Three necessary conditions for D2 are met -- the endpoint is creatable
unprivileged, the server role is obtainable, and conhost will serve an
endpoint f4 created. The remaining question is the one that decides the
direction: do the messages arrive, and do they carry what §8 claims they do
-- the application's intent, before anything wraps it.

**D2, third measurement (2026-08-28).** The endpoint's child objects --
`\Reference`, `\Input`, `\Output` -- all opened with `STATUS_SUCCESS`, so
the endpoint is a genuine console with real I/O objects, and conhost again
accepted a fresh (unclaimed) endpoint. But `GET_SERVER_PID` reported **no
client attached**: `cmd.exe` took the probe's own console, not ours.

The reason is worth recording, because it raises D2's price. A console client
does not pick its console from its standard handles. It attaches during
startup inside `kernelbase`, using the console handle in its inherited
`RTL_USER_PROCESS_PARAMETERS` -- which an ordinary `CreateProcess` fills with
*the parent's* console. Handing a child our `\Input` and `\Output` therefore
cannot redirect it. Windows itself does this from the kernel: `AllocConsole`
asks ConDrv to create the server process (the `0x500037` ioctl in the public
analyses). So being the server is not enough; f4 must also be able to *place*
clients on its endpoint, which is a chunk of what `AllocConsole` does.

The probe now tries every known route in a single run rather than one per
trip: standard handles (the failed baseline, kept so a report shows it
failing next to the others), the documented
`PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE` attribute, and having the probe attach
to the endpoint itself via `\Connect` so that a plain `CreateProcess` passes
that console down. It also captures a reference ConPTY session -- the bytes,
and what `mode con` reports as the width inside it -- so that whichever route
lands, the next step has something to compare console-API text against
without another measurement.

**D2, fourth and final measurement (2026-08-28): the direction is closed.**
All three attachment routes were tried in one run, and none put a client on
our endpoint. The decisive line is from route C:

    open \Connect: NTSTATUS 0xC0000008   (STATUS_INVALID_HANDLE)

The driver will not open the *client* side of a console relative to a server
handle. That side is not ours to create: the kernel makes it, when
`AllocConsole` asks ConDrv to create the process (the `0x500037` ioctl in the
public analyses). Route A failed as expected -- standard handles do not
choose a console. Route B "succeeded" at nothing relevant:
`CreatePseudoConsole` builds its own console with its own conhost inside, so
it was never pointed at our endpoint, and it is also what left an orphaned
console window on screen after the probe exited, since the pseudoconsole
outlived the child the probe killed.

So the tally for D2: the endpoint is creatable unprivileged (yes), the server
role is obtainable and exclusive (yes), conhost will serve an endpoint f4
created (yes) -- and f4 cannot place a client on it (no). Three of four, and
the fourth is the one that matters, because a console server with no clients
is furniture.

**Why the cheap version was an illusion.** D2's appeal was "do not
reimplement conhost, sit in front of it". But sitting in front requires
owning both sides of the conversation, and the client side is created by the
kernel on behalf of `AllocConsole`. To place clients f4 would have to drive
that undocumented ioctl itself -- that is, reimplement a piece of
`AllocConsole` on an interface with no compatibility promise, where a mistake
strands a console window on the user's desktop. The probe demonstrated that
failure mode by accident in its own last run. That is not a proxy; it is D
with worse guarantees and no C++ saved.

**What stands from the D2 work**, because it was not wasted:

- The seat is real and obtainable, and conhost accepts an endpoint f4
  creates. If direction D is ever built, f4 does not have to fight for the
  server role -- it can have it.
- The server role is exclusive, first claimant wins. Measured twice by
  control: conhost accepted the same handle before the probe claimed the
  role and refused it (`0x80070016`) after.
- The ConDrv control codes and the child-object names (`\Reference`,
  `\Input`, `\Output`) are confirmed working on 10.0.22000, and
  `condrv.sys` still reports a Windows 8-era file version, which is the
  strongest evidence available that this interface is not churning.

**Where this leaves the list.** D2 is closed. D -- build conhost's own
`src/host` into f4 -- keeps its appeal precisely because Microsoft already
solved the client-attachment problem inside it, and it remains gated on the
one-evening question of whether `src/host` builds standalone (O16). A first
for `wsl.exe` and PowerShell 7 is untouched by any of this and is still the
cheapest real improvement. E is what ships.

**E. Make the Terminal Log the answer.** It already holds logical lines, so
reflow there is free, and it may be all users need from history under
Windows. The honest minimum, and what ships today while A through D are
decided.

Order to try. **A first for `wsl.exe` and PowerShell 7**: there f4 is the
terminal, the wrap is its own by construction, and the work is nearly free --
it is the same thing f4's own SSH client already does for remote sessions,
with a different transport. **Then D2's probe run**, because it is the cheapest question with the
largest consequence: if f4 can hold the server endpoint and let the real
conhost do the work, the wrap stops being a guess and no C++ enters the
build. **D itself only if D2's seat turns out to be unavailable** -- it buys
the same fact at the price of a C++ console server. **C is demoted** to a special case inside A --
measured to work mechanically, but a 4000-column console changes what
width-aware programs *print*, not just how it is laid out (`ls -C` collapsing
to one 3658-character line), so it needs mode switching, and mode switching
is the guessing that §7 closed. **B only if D's route turns out to be shut**;
it buys winpty-grade behaviour, which is what Microsoft moved away from. **E
is what ships meanwhile.**
