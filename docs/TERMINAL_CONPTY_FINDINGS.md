# ConPTY findings, and a plan that stops fighting it

Index of everything known about the terminal, with status and the plan:
`TERMINAL_LEDGER.md`. Start there.

Sections 1-4 come from a probe run on a real machine (10.0.19045.7663,
`tools/conptyprobe`, log in issue #425) plus two field reports. **Section 5 is
a second build, 10.0.22000.2538, and it changes the conclusion of section 2.1.
Read it before acting on anything above.**

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
| 6 | Re-run `tools/conptyprobe` on a Windows 11 build | when a tester has one; only a newer build can reopen step 5 |


## 5. The second build: 10.0.22000.2538

Run with `f4probe` (issue #425), which models a cursor and a width instead of
splitting the stream on CRLF. That difference is not cosmetic: the first
version of that probe split on CRLF, saw a two-row wrapped line as one
140-character row, and reported four confident verdicts that were all
meaningless. On this build ConPTY often marks a line break with **no bytes at
all**.

### 5.1. There is no CRLF at the wrap point here

A 40-column console, one typed line whose echo is 65 characters. ConPTY sent
the 65 characters as one run and then moved with `ESC[9;1H`. The break between
row 1 and row 2 exists only because the terminal wrapped: nothing in the stream
says so, and nothing in the stream says otherwise either.

The repaint after narrowing back from 100 columns does the same thing: an
80-column line written whole into a 40-column console, with a CRLF only after
the last row.

**This is the opposite of §1 on 19045**, where the repaint cut the line at the
width and put a real CRLF there. Both are ConPTY; they differ by build.

### 5.2. Which means the join information is not always lost

If a row ends because it ran off the right edge, the next row continues it. If
it ends on a CRLF, it does not. On a build that behaves like 22000 that is not
a heuristic -- it is the actual structure, and `ExplicitLineBreak` can be
stamped from it with no oracle and no guessing. `ESC[K` agrees: it follows
short rows and never a full one, so the two signals corroborate each other.

On a build that behaves like 19045 the same reading is the winpty guess, wrong
once in W lines. The code must therefore not branch on a build number but on
what the stream is doing; see ledger item O8.

### 5.3. The oracle is feasible, and cheap

`ResizePseudoConsole(4000, 12)` is accepted. The repaint is about 287 bytes and
brings the long line back as one row. Two resizes per idle prompt is a few
hundred bytes and one round trip -- affordable at an idle prompt, and pointless
on a build where §5.2 already gives the answer for free.

### 5.4. There is no scrollback behind ConPTY

Thirty lines into a twelve-row console, then widen: the repaint starts at
LINE_21. conhost reflows and repaints what is on screen and nothing else. The
oracle can recover the *structure* of the viewport; it can never recover the
history. Step-zero question 1 from `TERMINAL_LEDGER.md` §3.3 is answered, and
the answer is no.

### 5.5. The repaint announces its own size

Every repaint opens `ESC[?25l ESC[8;<rows>;<cols>t ESC[H`. The XTWINOPS report
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
