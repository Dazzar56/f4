# Reflow: what other terminals do, and what f4 should copy

Index of everything known about the terminal, with status and the plan:
`TERMINAL_LEDGER.md`. Start there.

Companion to `TERMINAL.md` (architecture) and `TERMINAL_WINDOWS.md` (what is
broken on Windows and in what order it is being fixed). This file records a
survey of how other terminal emulators solve **reflow** — re-wrapping text when
the window changes width — so that the next person to look at it does not have
to repeat the reading.

**Status:** shipped on Unix, off on Windows.

A width change re-wraps the live primary grid: `unwrapLocked` joins the rows a
soft wrap split into logical lines and `reflowLocked` lays them out at the new
width (`cmd/f4/terminal_view.go`). A height-only change still takes the old
path, which moves rows between the viewport and `GridHistory` and was never
broken. Windows keeps Horizontal Preservation, gated by `terminalReflowEnabled`,
until the two experiments in section 3 have answers.

Verified by hand on `ls -la /usr/bin` with the window dragged narrow and wide
again, and covered by `TestTerminalView_Reflow_*`. Section 4 records what went
wrong on the way, because both defects looked like the reflow being a bad idea
rather than a bug in it.

## 1. far2l: the reference implementation to copy

The interesting code is **not** in `far2l/src/vt` (that is only the consumer).
It is in `WinPort/src/ConsoleBuffer.cpp` and `WinPort/src/ConsoleOutput.cpp`.

### 1.1. The wrap marker is on the cell, and it marks the hard break

`WinPort/WinCompat.h`:

    #define EXPLICIT_LINE_BREAK  0x0400  // Don't concatenate next line if this char is
                                         // last in current line when lines recomposed
                                         // due to screen resize or VT history rendering
    #define IMPORTANT_LINE_CHAR  0x0800  // Dont skip this character when recomposing
                                         // even if its a space

Two bits taken from the cell attributes. The polarity is the opposite of ours:
a row is a **continuation** of the next by default, and `EXPLICIT_LINE_BREAK` on
the last significant character says the line really ended. `f4` marks the soft
wrap instead (`WrapFlags[y] = true`).

The inversion is not cosmetic. A marker that lives on a character survives every
relayout of the grid: the character moves, the marker moves with it. `f4`'s
`WrapFlags[y]` is indexed by row and has to be shifted by hand on every scroll,
insert and delete — see `scrollUp`, `scrollDown` and `Resize` in
`cmd/f4/terminal_view.go`, all of which carry a parallel copy of the flags.

### 1.2. Cursor movement sets the marker, not the parser

`ConsoleOutput::DenoteExplicitLineWrap(pos)` is called from three places, all
gated on `ENABLE_PROCESSED_OUTPUT`:

* on `\r` (`ConsoleOutput.cpp:565`);
* on `\n` (`ConsoleOutput.cpp:569`);
* from `SetCursor`, when the cursor leaves the row or moves left
  (`ConsoleOutput.cpp:158-161`).

So: any cursor movement off a row marks that row's last significant character as
the end of a line. Text that reached the right edge and wrapped moved no cursor,
so no marker appears and the wrap stays soft. **No escape-sequence heuristics are
needed at all** — compare WezTerm, which guesses per printed character whether
ConPTY inserted a hard break (`enable_conpty_quirks`).

### 1.3. The reflow: flatten to a stream, lay it out again

`ConsoleBuffer::SetSizeRecomposing()`, about 90 lines:

1. For each row, find its real width by walking from the end past irrelevant
   spaces. A space is *not* irrelevant if it carries `EXPLICIT_LINE_BREAK` or
   `IMPORTANT_LINE_CHAR`, or if it changes the background colour — otherwise
   coloured fills would be eaten on shrink.
2. Append the significant cells of every row into one flat vector
   (`unwrapped_chars`). This is the "unwrap": the history becomes a stream of
   cells whose line structure is carried only by the markers.
3. Lay the stream back out at the new width, breaking on `EXPLICIT_LINE_BREAK`
   and on reaching the width.
4. Rows pushed off the top go out through `scroll_callback` into the scrollback
   (`VTLog`) — the role `GridHistory` plays in `f4`.

**The cursor rides through as an offset into the flat vector**
(`nc_cursor_offset`), not as a pair of coordinates, and is converted back at the
end. This is the whole answer to "how do you not lose the cursor".

### 1.4. Two load-bearing details

**Cumulative "important" marking.** Flattening is lossy for trailing spaces. Two
reflows in a row would put legitimate mid-line spaces into a trailing position
and eat them. So before flattening, every cell left of the trimmed space tail is
marked `IMPORTANT_LINE_CHAR`, which makes it survive the next flatten. The
marking accumulates over resizes. This part is a hack, and knowing it is a hack
is useful: it is the price of storing the line structure in markers rather than
in explicit line objects.

**Reflow is chosen by output mode, not by alternate screen**
(`ConsoleOutput::SetSizeInner`):

    if (_mode & ENABLE_PROCESSED_OUTPUT) {
        _buf.SetSizeRecomposing(...);   // flatten and lay out again
    } else {
        _buf.SetSizeSimple(...);        // just truncate
    }

A raw-mode TUI gets plain truncation and redraws itself. This is a better test
than `f4`'s current `UseAltScreen` check: a Python REPL raises no alternate
screen but wants no reflow either.

### 1.5. The scrollback joins wrapped lines too

`VTLog` stores each line compressed with a `FLAG_HAS_EOL`. When a new line is
added and the previous one had no EOL, the new one is **appended to its tail**
(`far2l/src/vt/vtlog.cpp:150-158`). Direct analogue of extruding into `f4`'s
`PieceTable` with `WrapFlags` respected.

### 1.6. What it costs

* Two attribute bits gone forever.
* Reflow is O(whole buffer) and rebuilds a vector on every resize.
* Lossy for trailing spaces, patched by the cumulative marking above.
* The code contains `fprintf(stderr, "ConsoleBuffer: cursor underflow\n")`, so
  the authors know the cursor can still slip in corner cases.

### 1.7. What it does not prove

far2l is a Linux port; there is no ConPTY anywhere in this path. It proves the
*architecture* works inside a file manager, and it proves the cursor can be
carried through a live-grid reflow. It says nothing about Windows.

## 2. Windows: ConPTY can hand reflow over to the terminal

`PSEUDOCONSOLE_RESIZE_QUIRK`, flag `0x2` to `CreatePseudoConsole`. Normally
ConPTY invalidates and re-emits the whole buffer on resize, which would overwrite
any reflow we did. With this flag it does not, and trusts the terminal to re-wrap
its own buffer. Windows Terminal's "Resize with Reflow" is built on it
(microsoft/terminal#4741); the prerequisite was #4415, which made ConPTY emit
soft-wrapped lines as actually wrapped.

**The risk to measure first.** The 2024 ConPTY rewrite (microsoft/terminal#17510,
"Goodbye VtEngine") states that `WriteCharsLegacy` now emits `\r\n` to force a
delayed EOL wrap, and that this breaks text reflow on resize; the alternative was
rejected because reading cells back from the buffer is lossy (UCS-2). So the
soft-wrap signal may be gone on recent Windows builds, and how well the flag
works depends on the conhost/OpenConsole version on the user's machine.

Also present: `PSEUDOCONSOLE_WIN32_INPUT_MODE` (0x4) and
`PSEUDOCONSOLE_PASSTHROUGH_MODE` (0x8, Windows 11 22H2+, relays the child's VT
almost uninterpreted). None of these are in the MinGW or older SDK headers, so
they have to be passed as numbers. Upstream `portable-pty` (what WezTerm builds
on) passes none of them; forks add them by hand.

| Project | Reflow | ConPTY handling |
| --- | --- | --- |
| Windows Terminal | yes | owns both ends of the pipe; `RESIZE_QUIRK` + its own `ResizeWithReflow` |
| WezTerm | yes | `enable_conpty_quirks`, per-character wrap heuristics, `INHERIT_CURSOR` |
| Alacritty | yes | pads with empty lines during resize |
| xterm.js (Tabby, Hyper, VS Code) | yes | resize throttling |
| far2l | yes | does not use ConPTY |

Everyone reflows under ConPTY; each does it with a different set of workarounds
over the same flag.

## 3. What this means for f4

The raw material is already here: `WrapFlags`, `GridHistory`, and a `PieceTable`
with a `WrapEngine`. Three differences from far2l, all worth adopting:

1. **Reflow as flatten-and-relayout, with the cursor as an offset into the
   stream.** Done. The cursor is carried as an offset inside its logical line,
   and the relayout never splits a wide character from its filler cell.
2. **Move the wrap marker from the row to the cell and invert it** — mark the
   hard break, not the soft wrap. Not done. `WrapFlags[y]` is indexed by row, so
   it has to be shifted by hand wherever rows move; a cell-level marker would
   delete every one of those parallel updates, because the marker would travel
   with the character.

   The bits are already reserved: `vtui/colors.go` declares
   `ExplicitLineBreak = 0x0400` and `ImportantLineChar = 0x0800`, the same names
   and the same values as far2l's `WinCompat.h`. Nothing in `f4` writes them
   yet. So this work needs no hunt for room in `Attributes` — it needs the
   careful part, which is that `WrapFlags` and `GridHistoryWrap` are touched in
   about three dozen places in `cmd/f4/terminal_view.go`, including `PutChar`,
   `scrollUp`, `scrollDown`, `EraseLine`, `EraseDisplay` and `GetAllLogBytes`.
   That code is shared with Windows and runs on every character ConPTY emits,
   and unlike the reflow itself it cannot be hidden behind
   `terminalReflowEnabled`: the whole point is for one marker to serve
   everywhere. Do it when the Windows terminal is not being tested, and expect
   to re-run the Windows checks afterwards.
3. **Decide reflow vs truncation by something better than the alternate
   screen.** Open, and it needs research before it needs code — the wording
   here used to suggest it was a matter of copying far2l, which it is not.

   far2l branches on `ENABLE_PROCESSED_OUTPUT`, a Windows console mode it owns
   because `WinPort` *is* the console API. On Unix the equivalent state (raw vs
   cooked, `ICANON`, `ECHO`) is set by the child on the slave side of the PTY
   and is not visible to us at all. The candidate substitutes are all
   heuristics with their own false positives: autowrap turned off, mouse
   tracking enabled, application cursor keys. So the question to answer first is
   an empirical one — which programs actually suffer from being re-wrapped while
   holding no alternate screen, and is that set large enough to be worth a
   heuristic. A Python REPL is the obvious candidate to measure; `readline`
   redraws its own line on `SIGWINCH`, so it may well not care.

### Order of work

| # | Step | Risk | Blocked by |
| --- | --- | --- | --- |
| 1 | Confirm the #409 fix on real Windows | — | users |
| 2 | Self-erasing cleanup of the directory sync (stop the upward creep) | low | 1 |
| 3 | Unix reflow: flatten and relayout | medium | done |
| 4 | Experiment: does ConPTY with flag `0x2` stop re-emitting the viewport | low | — |
| 5 | Experiment: does the soft-wrap signal survive on current Windows builds | low | 4 |
| 6 | Windows reflow | high | 3, 4, 5 |

Step 3 does not depend on Windows and can proceed in parallel. Step 6 is only
worth starting if steps 4 and 5 both come back yes; if the soft-wrap signal is
gone on current builds, the honest answer is to leave Windows as it is and record
that here as an external constraint.

### Open questions

* What `CreatePseudoConsole` does with flag `0x2` on Windows 10 1809 — ignore it
  or fail. A fallback is needed either way. `tools/conptyprobe` (build with
  `GOOS=windows go build ./tools/conptyprobe`) creates a pseudoconsole with
  flags 0 and 2 using the same calls as `pty_windows.go`, types a line longer
  than the console, resizes, and dumps the raw bytes; it answers this and the
  soft-wrap question above for whatever build it runs on. Two PowerShell
  attempts at the same experiment failed on P/Invoke details before reaching
  ConPTY. Results pending.
* What a full buffer reflow costs while the user drags the window border, and
  whether throttling (xterm.js, Fluent Terminal) is required.
* Which programs are actually harmed by being re-wrapped without an alternate
  screen — see item 3 above. Measure before adding a heuristic.

Answered since this file was written: `vtui.CharInfo` has room for a cell-level
marker, and the bits are already named for it (item 2).

## 4. What went wrong the first time

Both defects below shipped in the first version of the reflow and were found by
one `ls -la /usr/bin` and a drag of the window border. They are recorded because
neither looked like a bug at first — they looked like evidence that re-wrapping
the live grid was a bad idea.

**Trailing spaces are not padding on a wrapped row.** The first version trimmed
the trailing default-attribute blanks of every row before joining them, copying
far2l's trim without copying its precondition. A row that ended by itself may
well end in padding; a row that soft-wraps cannot, because text reached the
right edge, so every cell up to it was written. Those spaces are the column
alignment of the output, and trimming them glued the next row's first column
onto the previous row: `rootroot976304апр82024zsh`. `significantWidthLocked`
now takes the wrap flag and returns the full width for a wrapped row.

**Widening frees rows, and something has to fill them.** Re-wrapping to a wider
grid produces fewer rows than it consumed — that is the whole point — so the
viewport came back part empty while the output that would have filled it sat in
`GridHistory`. The height-only path pulls rows back for exactly this reason; the
reflow now does too, pulling until it has material for the new height instead of
only the half-lines that wrap into the top row.

The general shape of both: a reflow is not only a function of the visible grid.
It has to know which line breaks are real, and it has to be free to reach into
the history for the rest of a line — in either direction.

## The resize repaint, and why f4 drops it

f4 re-wraps its own grid from `GridHistory` when the width changes. ConPTY
then repaints *its* screen -- which has no history and only the rows that fit
the new size -- and if that repaint lands afterwards it replaces the recovered
rows with blanks. So on Windows, in `oracle` mode, the repaint is parsed into
a scratch view and dropped. Nothing is lost by dropping it: every row it
carries reached f4 once already, as output, before it scrolled (P16).

A chunk is that repaint when **all three** hold:

1. **Shape.** It opens with `ESC[?25l`, then the size report
   `ESC[8;rows;cols t` where the build sends one (P14), then a move to home
   (`ESC[H` or `ESC[1;1H`) -- a repaint redraws from the top. Command output
   positions at the row it is about to write, below home. Recognising a
   repaint by *timing*, or by the cursor hide alone, ate the middle of a `dir`
   listing, because ConPTY hides the cursor around every batch it writes
   (findings 6.18).
2. **ConPTY owes one.** One repaint is owed per `ResizePseudoConsole` call and
   paid by the next matching frame. A count, not a clock: a repaint trailing
   its resize by a second is still taken, and a program that repaints from
   home unprompted is not (6.19). The count is clamped at four.
3. **Not the alternate screen.** A full-screen program -- f4 running inside
   f4's terminal, an editor, a pager -- repaints from home exactly like a
   resize. On the alternate screen f4 does not re-wrap at all, so ConPTY's
   repaint is the only thing keeping that screen right and must land.

The frame is taken exactly: a read is classified from the front and the
rest is asked about again, so output coalesced before or after a repaint in
the same read reaches the display. A frame split across reads is held to its
`ESC[?25h` and no further; one with no close is abandoned past 1 MiB and the
stream returns to the display (findings 6.21).

## What the log says

`REFLOW:` at startup names the mode and the switches it set.
`REFLOW_ABSORB:` covers the first few absorbs of a burst and every fiftieth
after. `REFLOW_SUMMARY:` on shutdown and every fiftieth child resize gives
the mode, the resize and repaint counts, the repaints still owed, the oracle
passes, and the history's size in rows and characters. The detailed lines --
`REFLOW_WRAP`, `REFLOW_RESIZE`, `REFLOW_FRAME`, `REFLOW_SHOW` -- fire only
when something is unusual: a pass that destroyed characters, a resize path
that moved rows without re-wrapping, a frame that was not diverted or that
declared a stale size, and a screen drawn mostly blank over a non-empty
history.
