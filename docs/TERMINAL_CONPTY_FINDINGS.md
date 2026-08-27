# ConPTY findings, and a plan that stops fighting it

Index of everything known about the terminal, with status and the plan:
`TERMINAL_LEDGER.md`. Start there.

Sections 1-4 come from a probe run on a real machine (10.0.19045.7663,
`tools/conptyprobe`, log in issue #425) plus two field reports. **Section 5 is
a second build, 10.0.22000.2538, and it changes the conclusion of section 2.1.
Section 6 records the synchronized repeat and the paired classic-conhost / WT
run. Read both before acting on anything above.**

The original text of this file is left as it was written, because being able to
see what one build taught us -- and how confidently -- is the point of keeping
it. It replaces
guesswork in `TERMINAL_REFLOW.md` §2–3 with measurements, and the
measurements change the plan.

## 1. What the probe showed

Console 40 columns wide, one `echo` of 60 characters, then a resize to 100.

**The wrap point carries a hard CRLF.** ConPTY sent

    C:\2work-150>echo ABCDEFGHIJ0123456789ab<CR><LF>
    cdefghij0123456789ABCDEFGHIJ0123456789  <CR><LF>

That is a real line break at column 40, not a soft wrap. Nothing in the
stream tells a terminal that the two rows are one line. This is the regression
the 2024 ConPTY rewrite (microsoft/terminal#17510) admitted to, and it is not
theoretical: it is on a current Windows 10 build.

**`PSEUDOCONSOLE_RESIZE_QUIRK` is accepted and ignored.** `CreatePseudoConsole`
returned OK for flag `0x2`, and the bytes sent on resize were identical, to the
byte, to the run with flag `0`. The flag that Windows Terminal built its reflow
on does nothing here.

**ConPTY reflows the buffer itself and repaints the whole viewport.** On the
resize to 100 columns it sent 379 bytes: `ESC[?25l`, `ESC[H`, every row of the
screen with `ESC[K` after it, eleven `CRLF`s for a twelve-row console, then
`ESC[7;14H ESC[?25h`. And in that repaint the `echo` line came back **as one
row** — what had been three rows at 40 columns is a single row at 100. ConPTY
keeps the line whole internally and lays it out again on every resize.

Put together: the terminal on the other end of this pipe is not allowed to
know where the soft wraps are, is not allowed to keep ConPTY from repainting,
and does not need to reflow because ConPTY already has.

## 2. What this means for f4

### 2.1. Reflow on Windows: do not build it, receive it

`TERMINAL_REFLOW.md` §3 planned a Windows reflow gated on two experiments. Both
came back negative, and a third thing came back that makes them moot. The
viewport reflow is ConPTY's job and it does it. What f4 owes the user is the
*history* — the rows that scrolled off the top, which ConPTY no longer has —
and for those the soft-wrap signal is gone, so f4 cannot re-join them either.

So on Windows the visible screen reflows (ConPTY), and the scrollback does not.
That is the ceiling on this build, and it is set by the OS, not by us. Record it
as such; do not spend more on `RESIZE_QUIRK` unless a newer build proves to
honour it (the probe will say so in one run).

There is one thing f4 can still do for the scrollback, and it is cheap: when
ConPTY's repaint delivers the rows of the *current* screen re-joined, the rows
that had scrolled into `GridHistory` before the resize are now duplicated in
spirit but not in fact — the repaint only covers the viewport. Nothing to fix,
but the log view (F3) will show old lines at their old width and new lines at
the new; that is expected and not a bug.

### 2.2. The "creep": not the repaint (a retracted hypothesis)

Field report (Windows 11, "Proc"): with panels shown, entering and leaving any
folder moves the whole terminal history up one row, seen in Ctrl+O; the startup
banner drifts off the top. A second tester (Windows 10) did not see it on his
build, then reported it started with nightly 2462a68.

An earlier version of this section blamed ConPTY's full-frame repaint: H rows
joined by H−1 CRLFs, with the last CRLF supposedly scrolling the grid when the
cursor was already on the bottom row. **That is wrong.** Feeding the probe's
exact repaint frame into the parser for every height from 3 to 14 rows pushed
zero rows into the history at every height — the last CRLF lands on row H−2
and moves the cursor to H−1, and `newline()` never runs off the end. The
hypothesis is retracted; the test written for it was deleted rather than kept
as a false guard.

What is left, by elimination: the creep tracks folder changes, and a folder
change is the one thing that types into the shell (`cd /d "…" & rem
f4_sync`), which makes cmd echo the line, print a blank line and print a new
prompt — two or three rows per folder, exactly the creep described in #165.
The stream excision erases the echoed text but not the rows. Why one tester
sees it and another does not is unexplained; 2462a68 as an onset is a clue for
whoever bisects it. See the ledger, open item O1.

### 2.3. Colour bleed on the padded rows (microsoft/terminal#75)

Related and long-known: when conhost pads a reflowed row with spaces it gives
the padding the attributes of the last non-blank character, so a colour or
underline runs to the end of the row after a resize. It is upstream and old
(2017). Not ours; noted so nobody hunts for it in f4's renderer.

## 3. The other terminals, seen again with the measurements in hand

- **Windows Terminal** — reflows because it owns conhost and can make
  `RESIZE_QUIRK` mean something. On a build where the flag is a no-op, so is
  the approach.
- **WezTerm** — per-character heuristics about whether ConPTY inserted a hard
  break (`enable_conpty_quirks`). The probe shows what those heuristics are
  fighting: at the wrap point the bytes are indistinguishable from a real
  line break. That is why it is a heuristic and why it is off by default.
- **Alacritty / xterm.js** — pad and throttle; neither recovers the soft-wrap
  signal because it cannot be recovered.
- **far2l** — no ConPTY. Its cell-level `EXPLICIT_LINE_BREAK` is set from
  cursor movement, and here the cursor movement *is* a `CRLF`, so the same
  scheme would mark ConPTY's forced wraps as hard breaks too. It has no answer
  for this either; it simply never faces it.

None of them has a trick we missed. The stream does not contain the
information.

## 4. Plan

| # | Step | Status |
| --- | --- | --- |
| 1 | Settled-prompt completion, children, screen-at-settle (`TERMINAL_WINDOWS.md`) | shipped |
| 2 | ~~Repaint bracket~~ — retracted, the repaint does not scroll (§2.2) | dropped |
| 3 | Self-erasing directory sync cleanup | next — the sync is the remaining creep suspect |
| 4 | Stop resizing ConPTY for the keybar (§2.2 option 2) | later, for flicker |
| 5 | Windows reflow of the live grid | **dropped** — ConPTY does it; the flag is a no-op. The history's join information is recovered instead, behind `F4_WIN_REFLOW` (`TERMINAL_LEDGER.md` §3.3.1) |
| 6 | Re-run the cursor-model probe on Windows 11 24H2/25H2 | pending field run; paired classic-conhost / WT runs on 22000 are complete, but only a newer ConPTY can reopen step 5. The in-tree `tools/conptyprobe` now performs the forced-conhost, explicit-WT and default-terminal-handoff runs itself and needs no user-set variables. |


## 5. The second build: 10.0.22000.2538

Run with `f4probe` (issue #425), which models a cursor and a width instead of
splitting the stream on CRLF. That difference is not cosmetic: the first
version of that probe split on CRLF, saw a two-row wrapped line as one
140-character row, and reported four confident verdicts that were all
meaningless. On this build ConPTY often marks a line break with **no bytes at
all**.

### 5.1. The live stream and the repaint disagree with each other

Measured with a cursor model, a 40-column console, one line of exactly 40
characters and one of 60:

| | how the long line's rows end |
| --- | --- |
| live stream | `wrap, lf, lf` -- a real CRLF at the break |
| repaint after narrowing 100 -> 40 | `wrap, lf` -- no CRLF at the break |
| repaint after narrowing 40 -> 20 | `wrap, wrap, lf, wrap, wrap, lf` |

So it is not a build that "has" or "lacks" the signal. The **live** stream
breaks the line itself, as §1 saw on 19045. The **repaint** does not: it writes
the logical line whole and lets the terminal's autowrap place it, and only the
last row carries a CRLF.

An earlier draft of this section claimed the opposite for the live stream. It
was written from a hand-read of one chunk in which cmd happened to move with an
absolute CUP instead, and it was wrong. Both shapes occur; only a cursor model
tells them apart, which is why the probe now carries one.

### 5.2. `ESC[K` helps, but its exact-width ambiguity is real here

`ESC[K` appeared after every **short** row that ended with a break and after no
row that ended by wrapping. But the synchronized run's raw 100 -> 40 repaint
also contains the deliberate control case: forty `A` characters, then CRLF,
with no `ESC[K`. That is a hard-broken line exactly as wide as the console,
and the winpty guess -- "a full row with no `ESC[K` is a continuation" --
would join it incorrectly.

The earlier statement that this failure case did not occur came from the
probe's `wrap.el_on_broken_row` summary. That verdict examined the marked long
`B` line only; it did not include the exact-width `A` control even though the
raw grid report did. The cursor model can distinguish `wrap` from `lf` in this
22000 repaint, but the `ESC[K` hint alone is still a guess. The oracle can
correct viewport rows; rows already outside ConPTY's viewport retain the
one-in-width ambiguity.

### 5.3. The oracle is feasible, and very cheap

`ResizePseudoConsole(4000, 12)` is accepted. The repaint is 287 bytes and
brings the long line back as one row. Timed from the resize call: **first byte
after 8 ms, frame complete at 8 ms, the whole round trip out and back 9 ms.**

That number matters. An earlier run reported 509 ms, which was the probe
measuring its own quiet window and nothing else. At 9 ms the oracle is
affordable at every idle prompt, not just occasionally.

### 5.4. There is no scrollback behind ConPTY

Thirty lines into a twelve-row console, then widen: the repaint starts at
LINE_21. conhost reflows and repaints what is on screen and nothing else. The
oracle can recover the *structure* of the viewport; it can never recover the
history. Step-zero question 1 from `TERMINAL_LEDGER.md` §3.3 is answered, and
the answer is no.

### 5.5. The repaint announces its own size

Every repaint opens `ESC[?25l ESC[8;<rows>;<cols>t ESC[H` -- measured
verbatim as `ESC[8;12;100t` for a resize to 100 columns. The XTWINOPS report
was there all along and nothing in f4 reads it. It is a better frame delimiter
than the cursor-visibility pair, because it also says *which* size the frame
describes -- so a frame can be matched to the resize that asked for it.

### 5.6. Flags: still nothing

`PSEUDOCONSOLE_RESIZE_QUIRK` (0x2) and passthrough (0x8) are both accepted and
both produce byte-identical output to flag 0. Two builds, two flags, no
observable effect. Stop spending on them.

### 5.7. The title comes through after all

`TERMINAL_LEDGER.md` C4 was retired because P3 showed the console title is not
readable by enumerating windows behind a pseudoconsole. That is true and
irrelevant: cmd's `<title> - <command>` arrives in the VT stream as OSC 0, for
external programs and for batch files alike, and the bare title comes back when
the command ends. That is a completion signal that does not depend on `PROMPT`
at all -- which is exactly what a batch running `prompt $P$G` takes away from
us today.


## 6. What the first cursor-model run did *not* establish

The probe typed the OSC 133 `PROMPT` after `timeout /t 3 /nobreak` and waited
for the stream to go quiet rather than for the process to exit. `timeout`
counts down without printing, so the wait ended early and the `prompt` command
was swallowed by the still-running child. Everything downstream of that in the
run -- mark passthrough, mark order, the batch mark counts -- was measured with
no marks configured and means nothing. Those questions are still answered only
by the 19045 and 26200 field reports (P1, P2) and by an earlier 22000 run that
agreed with them.

Recorded here because a log full of `= 0` reads like a finding, and this one is
not one. The probe should wait on the child, not on silence.

### 6.1. The synchronized follow-up closes that hole

`f4probe 4` was run again on the same 10.0.22000.2538 machine. This version
waited for the process rather than the stream: it saw the direct
`timeout.exe` child 32 ms after sending the line, saw that exact PID disappear
3.067 s after the line was sent, and only then changed `PROMPT`. The dependent
measurements are therefore valid in this run:

- OSC 133 marks pass through ConPTY, and 22000 delivered the prompt-end mark
  before the prompt text (`mark_before_prompt_text`), agreeing with 19045;
- the ECHO-on batch produced four prompt-end marks, including the marks forged
  by its echoed lines;
- the ECHO-off batch produced one final prompt-end mark;
- the batch that executed `prompt $P$G` produced one mark before that command
  took effect and no marks for its later lines or final prompt.

The title measurements were also repeated after the synchronization fix. OSC
0 carried the busy form while `timeout.exe` ran and the bare title after its
confirmed exit. A batch carried the busy form for its whole lifetime,
including while its in-process `pause` had no child process at all. This closes
the measurement gap above; it does not by itself implement the title-based
completion path in f4.

### 6.2. The summary missed the exact-width control

The follow-up also exposed the reporting error behind the old §5.2 claim. In
the raw 40-column repaint the exact-width output is shown as a full row ending
in `lf` with `ESC[K=false`. The summary still says
`wrap.el_on_broken_row=yes` because that key is computed from the long-line
verdict only. `f4probe 5` reports the exact-width line's end, hard-CRLF state,
`ELOnBreak` and whether the hint would join it as separate summary keys. This
log is already sufficient to correct the finding; the new keys prevent the
same reading error on 24H2/25H2.

### 6.3. The Notepad cleanup result is not a process-topology finding

The same run opened a visible Notepad, but the probe found neither a direct
child of cmd nor a descendant within three generations, and consequently
reported `notepad processes closed by the probe: 0`. The tester closed the
window manually after the probe had finished. The launch may have been
brokered or may have reused an application process; this run did not determine
which.

That does not affect any ConPTY, repaint, prompt, batch or title result:
Notepad was the last scenario. It does mean that `notepad` cannot be used as a
portable assertion about parent/child topology, and that a future probe must
identify only newly created Notepad processes outside the shell tree if it
wants to clean them up safely.

`f4probe 5` does that conservatively: it skips the scenario if any Notepad
process already exists, otherwise compares the global `notepad.exe` PID set
before and after launch, records the actual parent chain and windows, and
terminates only newly observed PIDs. A missing new PID is logged and left
untouched rather than risking another application instance.

In both paired version-5 runs it found two new Notepad PIDs: a direct child of
cmd and its child. It closed 2/2 in both cases, so no window was left behind.
The contrast with the earlier run is the finding: the new cleanup works when
the launch is observable, while Notepad parentage itself remains unsuitable as
an invariant.

### 6.4. The outer host is separate from the ConPTY being tested

`f4probe 5` was run twice on the same 10.0.22000.2538 installation, once in a
classic conhost window and once in Windows Terminal. The classifier did
separate them:

- classic conhost: no `WT_SESSION`, `ConsoleWindowClass`, 960x480 client,
  DA1 `ESC[?1;0c`, no sixel;
- Windows Terminal: `WT_SESSION` set, `PseudoConsoleWindow`, 0x0 client owned
  by `CASCADIA_HOSTING_WINDOW_CLASS`, DA1 containing parameter 4, so sixel is
  advertised.

DA2 was `ESC[>0;10;1c` in both. Neither host answered XTVERSION or the two
sixel-geometry queries; silence there does not override WT's positive DA1.

The probe creates a separate hidden pseudoconsole for its cmd scenarios. The
entire cursor-model section -- flag 0, the live stream, all resize repaints,
the wide oracle, scrollback test and flags 0x2/0x8 -- is byte-for-byte
identical between the two logs. All prompt, OSC 133, title, batch and child
verdicts agree too, although one ECHO-on batch transcript used a different
but semantically equivalent cursor/CRLF layout. Thus the outer host changes
the capabilities available to f4 itself; it did not change the measured child
ConPTY behaviour on this build.

Both runs still report Windows 11 21H2, build 22000. They answer the paired-host
question, not the outstanding 24H2/25H2 build question.

### 6.5. The version-6 real-f4 run found the integration bug

The automatic version-6 run completed all three launch contexts on the same
10.0.22000.2538 machine. Forced conhost was classified as classic conhost and
explicit `wt.exe` as Windows Terminal. The clean new-console launch selected
classic conhost too; both default-terminal delegation registry values were
unset. Its standard handles were redirected by the controller, so its missing
DA1 answer is not a terminal capability result. Window and process topology
are sufficient for the host classification.

The adjacent real f4 was then exercised in `off`, `hint`, `oracle` and `probe`
modes. Startup, outer resizes, nested-cmd Enter and private directory-sync
excision all ran in every mode. The initial probe summary called that
`complete`, but the debug logs expose a narrower and more important result:
all five oracle passes and all five probe passes captured delimited wide and
narrow frames, then rejected them because row *y* of ConPTY's repaint did not
equal row *y* of f4's display. Every pass logged `nothing stamped`.

That is not a failure of the resize oracle measured in §6.2. f4 had already
moved some rows into `GridHistory`, while ConPTY still repainted them, and f4's
private cmd-sync excision intentionally removes rows which remain in ConPTY's
buffer. The matcher was comparing two different vertical slices. The fix is
to align exact consecutive row pairs against f4's combined
`GridHistory + viewport` journal and stamp those journal rows. Besides fixing
the offset, this is the way around ConPTY's viewport ceiling: once a row's
boundary is confirmed, the flag follows that row through local scrollback and
into the permanent log even after ConPTY has discarded it. Repeated or
isolated rows are not anchors; the conservative hint remains for boundaries
which no oracle frame ever overlaps.

### 6.6. The corrected version-6 run: the oracle works, the runner did not

A second `f4probe 6` run on the same 10.0.22000.2538, with the journal matcher
of §6.5 in place, shows the oracle doing what §5.3 predicted. Across the two
modes that run it, ten passes captured delimited wide and narrow frames, and
the ones that aligned stamped real boundaries: in `oracle` mode five passes
stamped 2, 2, 4, 4 and 1 boundaries and reported `0 became stale`, with `0
where hint and oracle disagree` every time. So on this build the `ESC[K` guess
of §5.2 was never wrong where the oracle could check it. That is a measurement
of the guess, not a licence to trust it: the disagreement count is only
meaningful over the rows an oracle frame actually overlapped.

Two of the four modes were nonetheless reported `incomplete`, and both were
faults in the runner rather than in f4:

- `off` was failed for `startup=0`. The runner drained the pseudoconsole with
  a fixed quiet window and f4's first flush arrived after it, on a machine
  where plugin loading takes longer than the window. The same run recorded
  114 KB of screen output afterwards, so the launch plainly succeeded. The
  runner now waits for f4's own `F4 STARTUP` line in the debug log before
  draining, and the verdict accepts the union of independent startup signals.
- `probe` was failed because one pass logged `nothing stamped`. That pass had
  aborted on `display changed during the pass`, which is the safety rule of
  §3.3.1 working: an oracle pass that cannot prove the frames describe the
  same text must stamp nothing. The verdict required *zero* rejected passes,
  which asks the safety rule never to fire. It now requires at least one pass
  that stamped, and reports stamped and safely-rejected passes as separate
  numbers.

The lesson is worth more than the fix: a verdict that treats a conservative
refusal as a failure will keep reporting a working mechanism as broken, and
the log will look like evidence against the design.


### 6.7. Field report: the scrollback does not come back -- read from the log

Observed by the maintainer on 10.0.22000, in **both** classic conhost and
Windows Terminal: rows that scrolled off are not restored on a widen; a
correct-looking history flashes during the resize drag and is replaced; after
shrinking a long way and expanding again the freed area stays black.

A `--debug` log of that exact sequence was taken, and it answers more than the
symptom. **The run was made with `F4_WIN_REFLOW=probe`, and in that mode f4
does not re-wrap anything by design.** `terminalReflowEnabled` is false on
Windows and `ReflowOnResize` is set only for `winReflowOracle`
(`panels_frame.go`), so `TerminalView.Resize` never reaches `reflowLocked`:
`probe` is documented as "stamps nothing", and not re-wrapping is the same
decision applied to the resize path. The log agrees exactly: across 488
`FM_RESIZE` events there is not one reflow, only 84 more
`ScrollUp extruding row 0 to history` and zero rows pulled back down.

So the first thing this log establishes is that **the symptom was measured in
the one mode that cannot show the feature**, and the retraction is cheaper than
any fix: repeat the sequence with `F4_WIN_REFLOW=oracle`, which is also the
Windows default and therefore what users actually run.

Two things it establishes regardless of mode, because they do not depend on the
re-wrap:

- **The oracle almost never fires in a working session.** It ran twice in the
  whole log: once at startup (`1 safe boundary`) and once aborted by its own
  safety rule (`display changed during the pass`). During the entire resize it
  ran zero times, because it only fires at a settled prompt with no console
  child and a resize drag never offers one. Whatever the re-wrap does, the rows
  it works on carry boundaries from the `ESC[K` hint and essentially never from
  the oracle. That is not a failure of the oracle -- §6.6 measured it agreeing
  with the hint everywhere it could check -- but it does mean the hint, not the
  oracle, is what the history's correctness rests on in practice.
- **Nothing is fighting over the seam.** The hypothesis in the previous version
  of this section -- ConPTY's repaint landing and losing to f4's redraw -- has
  no support in the log: there is no repeated repaint of the same rows, just
  extrusion into history. The flash during the drag is ConPTY's own reflowed
  frame arriving and not being kept, which is exactly what a mode that does not
  re-wrap should look like.

**Next step, in this order, and stop at the first one that explains it.**

1. Re-run the same resize sequence with `F4_WIN_REFLOW=oracle` and `--debug`.
   If the history comes back, the report is a mode artefact and the remaining
   work is documentation plus deciding whether `probe` should re-wrap.
2. If it still does not, the log now has a specific question: does
   `reflowLocked` run at all (it is reached only on a **width** change -- the
   drag in this log changed width and height together, and every step went
   through `Resize`), and does it pull rows back from `GridHistory` when the
   viewport grows (A6, the Unix path already does this)? The zero
   `ScrollDown` count is the thing to explain.
3. Only then consider the black area after a large shrink and re-expand, which
   is the "widening frees rows and nothing refills them" symptom (A6) and may
   be the same bug or a second one.

The `REFLOW:` line now names the switches rather than the mode alone
(`hint_wrap`, `rewrap_on_resize`, `oracle_passes`), and a mode that will not
re-join the scrollback says so in the log next to the way to turn it on. Step 1
above would have been unnecessary with that line present, which is the whole
reason it exists.


### 6.8. The oracle works; ConPTY's repaint overwrites what it recovers

A second field log, this time in the default `oracle` mode (the new `REFLOW:`
line confirms it: `hint_wrap=true rewrap_on_resize=true oracle_passes=true`),
resolves the report of §6.7. The parts we built are not the problem.

**The oracle is fine.** Its second pass in that session aligned 27 of 29
repaint rows and stamped **25 boundaries**, with `0 became stale` and `0 where
hint and oracle disagree`. Whatever is wrong, it is not the matcher, not the
journal alignment and not the stamps.

**f4's history is fine.** 4728 rows were extruded into `GridHistory` during the
`dir` that filled the screen. Nothing was lost on the way in.

**What overwrites it is ConPTY.** Across 444 resize events the pty delivered
**197 full-viewport repaints**, each opening
`ESC[?25l ESC[8;<rows>;<cols>t ESC[H` and rewriting every row. f4 re-wraps its
grid from history on the width change, and then the next repaint lands on the
display and replaces those rows with ConPTY's own. That is exactly the reported
"the correct history flashes and is immediately replaced": the flash is f4's
result, visible until the following frame.

**And that explains the blackness.** ConPTY keeps no scrollback (P16): its
buffer is the viewport and nothing more. Shrink the window to 24 rows and its
buffer holds 24 rows; expand again and it repaints those 24 rows plus blank
space, and the blank space overwrites everything f4 had recovered from its own
history. The rows are still in `GridHistory`; they are simply painted over.

So the question is not how to recover the line structure -- that is solved and
measured -- but **who owns the viewport during a resize**. Today ConPTY has the
last word and f4 has the data.

**Next step.** This is what §3.3 already prescribes in its last paragraph, now
with a log behind it: on a width change, ConPTY's repaint must be *accepted as
structure rather than applied as pixels* -- parsed, matched against
`GridHistory + viewport`, and stitched at the seam, with f4's history winning
above the seam. The frames are unambiguous to find: every one of the 197
carries the XTWINOPS size report `ESC[8;<rows>;<cols>t` (P14), which names the
size the frame describes, and nothing in f4 reads it yet (O9). Reading it is
the prerequisite, not a nicety: without it there is no way to tell a resize
repaint from ordinary output, and no way to know which resize a late frame
belongs to.


### 6.9. The first half of the fix: stop letting the repaint land

Implemented: on a **width** change in `oracle` mode, ConPTY's repaint is parsed
into a scratch view and dropped instead of being applied to the display
(`reflowOracle.absorbResizeRepaint`, called from `PanelsFrame.handleResize`
after `TerminalView.Resize` has re-wrapped the grid).

Why dropping is safe rather than lossy: every row that frame carries already
reached f4 once as ordinary output, before it scrolled, and is in
`GridHistory`. The frame adds nothing and subtracts the history above it. Only
`oracle` mode does this, because only there does `ReflowOnResize` give f4's own
re-wrap ownership of the viewport; in every other mode the repaint is the one
thing keeping the screen correct and must land. It also never takes the stream
from an oracle pass, and it closes on `ESC[?25h` or after 250 ms, so ordinary
output is never swallowed.

What this does not do yet, and should not be mistaken for: it does not *read*
the frame. The stitching described in §3.3 -- match the repaint against
`GridHistory + viewport` and use it as structure at the seam -- still wants
O9, the XTWINOPS size report, so a frame can be tied to the resize that caused
it and told apart from ordinary output. Dropping is the conservative first
half; the field run says whether the seam is now right or merely no longer
overwritten.


### 6.10. The re-wrap is lossy, and a resize drag runs it hundreds of times

The `REFLOW_WRAP:` line added for this question answers it on the first field
log, and the answer is not the one any hypothesis so far predicted. The history
is not failing to come back. **It is being destroyed, a little at a time, by
the re-wrap itself.**

Read the logical-line count across one drag (each line is one `reflowLocked`
call, 183 of them in this run):

    history 2000 -> 1885; 1858 logical lines
    history 1885 -> 1877; 1856 logical lines
    ...
    history 1792 -> 2000; 1811 logical lines
    history 2000 -> 2000; 1520 logical lines
    history 2000 -> 2000; 1025 logical lines
    history 2000 -> 2000;  937 logical lines
    history 2000 -> 2000;  817 logical lines
    history 2000 -> 2000;  744 logical lines
    ...
    history    9 ->    0;   10 logical lines
    history    0 ->    0;    1 logical line

`unwrapLocked` collects the history correctly -- that half was never wrong. But
the number of *logical lines* it reconstructs falls on almost every pass, from
1858 to 744 and eventually to one. The rows are not being pushed back out of
the viewport; they are ceasing to exist. `GridHistory` reaching its 2000-row
cap hides this at first (the count is pinned at the cap while the content
underneath thins out), and then the cap stops being reached at all and the
history collapses to zero.

That explains every symptom in one stroke, including the ones the previous two
patches did not touch:

- rows do not come back on a widen because by then they are gone;
- the flash during the drag was real content, destroyed by the next pass rather
  than overwritten by ConPTY (which the absorber has already ruled out: it
  fired 174 times in the previous run with no change to the symptom);
- a large shrink and re-expand ends in blackness because a long drag is
  hundreds of passes, and the loss compounds until nothing is left.

It also explains why this is a Windows-only report despite `reflowLocked`
being shared code: on Unix a resize delivers one reflow, while a ConPTY drag
delivers one per pixel step -- 183 here, 575 `FM_RESIZE` events. A pass that
loses a fraction of a percent is invisible once and fatal five hundred times.

**Next step, and it is now a narrow one.** `reflowLocked` and `unwrapLocked`
must be shown to be lossless in isolation, before anything else is attempted:
a test that builds a known viewport plus history, re-wraps it through a
sequence of widths (including width-and-height changes together, as a drag
sends), and asserts that the logical text is identical at every step and at the
end. The field log says the invariant is violated; the test says where. Likely
suspects, in the order the code makes them reachable: `lastRow` in
`unwrapLocked` truncating at the last row with text while the cursor sits above
real content below it; `significantWidthLocked` trimming cells that a wrapped
row still needs; and the interaction with the 2000-row cap when the re-wrap
produces more rows than it consumed. None of these is confirmed -- the
measurement to make is the round-trip test, not another field run.
