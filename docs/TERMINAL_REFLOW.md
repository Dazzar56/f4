# Reflow: what other terminals do, and what f4 should copy

Companion to `TERMINAL.md` (architecture) and `TERMINAL_WINDOWS.md` (what is
broken on Windows and in what order it is being fixed). This file records a
survey of how other terminal emulators solve **reflow** — re-wrapping text when
the window changes width — so that the next person to look at it does not have
to repeat the reading.

**Status:** the Unix half is implemented — see `terminalReflowEnabled` and
`reflowLocked` in `cmd/f4/terminal_view.go`. A width change now re-wraps the
live primary grid; a height-only change still takes the old path, because
that one is about moving rows between the viewport and `GridHistory` and was
never broken. Windows keeps Horizontal Preservation until the two experiments
in section 3 have answers.

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
   stream.** Done: `unwrapLocked` joins soft-wrapped rows into logical lines,
   reaching back into `GridHistory` for a line that wrapped across the top
   edge, and `reflowLocked` lays them out at the new width. The cursor is
   carried as an offset inside its logical line.
2. **Move the wrap marker from the row to the cell and invert it** — mark the
   hard break, not the soft wrap. Not done. `WrapFlags[y]` still has to be
   shifted by hand in `scrollUp`, `scrollDown` and `Resize`; a cell-level
   marker would delete all three of those parallel updates.
3. **Choose reflow vs truncation by output mode**, not only by alternate
   screen. Not done: a raw-mode TUI that raised no alternate screen (a Python
   REPL) is still re-wrapped under it.

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
  or fail. A fallback is needed either way.
* What a full buffer reflow costs while the user drags the window border, and
  whether throttling (xterm.js, Fluent Terminal) is required.
* Whether `vtui.CharInfo` has room for a cell-level marker or it needs a parallel
  array.
