# Editor highlighting: background state checkpoints

Working document for issue #458 ("Тормоза в редакторе").
Status is kept at the bottom; update it at the end of every step.

## 1. The problem

Opening a huge file (e.g. `objdump -d install/far2l > far2l.s`) and pressing
`Ctrl+End` freezes the editor for 30-40 seconds. Leaving and re-entering the
editor makes it happen again.

The piece table is not involved. The cause is in `editor_view.go`,
`DisplayObject`, the "Stateful Highlighting" block:

```go
if ev.highlighter != nil {
    for len(ev.lineStates) <= logIdx {
        currIdx := len(ev.lineStates)
        ...
        attrs, nextState := ev.highlighter.Highlight(string(lineData), prevState, bgAttr)
        ev.lineStates = append(ev.lineStates, nextState)
    }
}
```

`ev.lineStates` is a contiguous array starting at line 0: `lineStates[i]` is the
highlighter state *after* line `i`, and the state passed into line `i` is
`lineStates[i-1]`. To draw line N the loop must therefore run the highlighter
over every line before it — synchronously, inside the draw path.

`Ctrl+End` sets `CursorLine = LineCount()-1`, so the next frame starts drawing
near the last line and the loop highlights the whole file in one go.

Consequences that match the report:

- the choice of highlighter does not matter — Chroma and Colorer are both O(N)
  over the file;
- re-entering the editor repeats the wait: a fresh `EditorView` has an empty
  `lineStates`, and the restored cursor position is again at the end;
- editing near the top of the file and then jumping to the end repeats it too,
  because `invalidateStates(line)` truncates the chain at the edited line;
- `colorer_plugin.go` calls `invalidateStates(0)` when the session or scheme
  becomes ready, with the same effect.

## 2. What we are building

A state chain is unavoidable for a stateful highlighter: colouring line N
correctly requires having parsed lines 0..N-1 at least once. The goal is
therefore not to avoid the work but to

1. **never do it on the draw path** — the UI must stay responsive;
2. **do it once per file** and keep sparse *checkpoints*, so that any later
   jump only has to replay a bounded number of lines;
3. **degrade visibly, not silently** — a region whose state is not known yet is
   drawn unhighlighted and is repainted when the walker reaches it.

Target structure:

```
checkpoints[k]   = highlighter state entering line k*Step   (checkpoints[0] = nil)
validLines       = number of lines already walked (states known for [0, validLines))
window{base, states[]}  = dense states for the currently visible region
```

`Step` starts at 1000 lines. Drawing line L needs the state entering L:

- `L < validLines` → take `checkpoints[L/Step]`, replay at most `Step` lines
  into the window (single-digit milliseconds), draw with colours;
- `L >= validLines` → draw without colours and let the background walker
  continue; repaint when it passes L.

The walker is driven by `vtui.FrameManager.PostTask` in short time slices, the
same way `StartIndexing` feeds the line index. This is deliberate: highlighters
are not thread-safe, and the Colorer session is a pooled external resource.
Slices give responsiveness without concurrency.

## 3. Constraints and traps

- **`vtui.Highlighter` is an external interface.** Signature:
  `Highlight(line string, prevState any, baseAttr uint64) ([]uint64, any)`.
  We cannot add methods to it; anything extra has to be an optional interface
  discovered with a type assertion.
- **Colorer's "state" is not a state.** `ColorerHighlighter.Highlight` derives
  the line number from `prevState.(int)`, and the real parser state lives in a
  forward-only `colorer.Session`. `resync` can only move forward; going
  backwards triggers `Reset()` + re-parse from line 0. So for Colorer:
  - a forward sequential walk is fine and incremental,
  - replaying from a checkpoint *backwards* is not cheap and will need its own
    step (phase 5),
  - `ch.lines` accumulates every line of the file as a Go string — a real
    memory problem on a 100+ MB file, also phase 5.
- **The background line indexer runs in parallel.** `li.LineCount()` grows
  while we walk. The walker must tolerate that and must be cancelled and
  restarted like the indexer is (`editSession` counter, `ctx`).
- **Invalidation granularity.** `invalidateStates(fromLine)` is called from ~15
  places on every edit. It must stay cheap: truncate `validLines` and drop
  checkpoints above `fromLine/Step` only.
- **Tests.** `editor_view_test.go` (lines ~60-125) pokes `ev.lineStates`
  directly. Those assertions have to move to whatever replaces it; keep the
  behaviour they check (chain is built, invalidation truncates it).

## 4. Plan

Each phase is one commit, one patch, verified before the next one starts.

### Phase 0 — seam
Move the state-chain logic out of `DisplayObject` into a `stateCache` type in a
new file `editor_states.go`. Behaviour identical, still synchronous, still
building from line 0. `lineStates` becomes `states.at(i)`; the tests follow.
Purpose: a single place to change in every later phase.

### Phase 1 — stop the freeze
Give `stateCache` a per-call budget (a few thousand lines). If the requested
line is out of reach, report "not ready" and let `DisplayObject` draw the
visible fragments with base attributes only. After this phase `Ctrl+End` is
instant on `far2l.s`, initially without colours.

### Phase 2 — background walker
A `PostTask` loop that keeps extending the chain toward the line the view
actually needs, ~5 ms per slice, with cancellation on close/edit/reload and a
generation counter. `Redraw()` when the walked region reaches the viewport.

### Phase 3 — checkpoints
Stop keeping one `any` per line for the whole file. Keep `checkpoints` every
`Step` lines plus a dense window around the viewport. Replay from the nearest
checkpoint on jumps. Map `invalidateStates` onto checkpoint granularity.

### Phase 4 — feedback
Show walker progress in the top bar (e.g. `HL 42%`) while a visible region is
still uncoloured, so the user sees work in progress instead of plain text.

### Phase 5 — Colorer
Backward jumps and `ch.lines` growth. Options to evaluate then: bounding
`ch.lines` to the walked window, a dedicated session for the walker, or asking
the colorer session for a restorable state.

### Phase 6 — optional fast path
During the walk we only need the state, not the attribute slice. If the
highlighter also implements something like
`HighlightState(line string, prev any) any`, use it and skip the per-line
allocation. Chroma-side support lives in vtui, so this may become an upstream
change.

## 5. Status

- [x] Phase 1 — instant unhighlighted rendering on distant jumps (0ms initial delay)
- [x] Phase 2 — throttled background walker (non-blocking state catch-up)
- [x] Phase 2a — slices bounded by wall clock, not by line count; walker yields
      to the line indexer while a file is still being opened
- [ ] Phase 0 — seam
- [ ] Phase 3 — checkpoints
- [ ] Phase 4 — feedback
- [ ] Phase 5 — Colorer
- [ ] Phase 6 — optional fast path

**Current step:** phase 2a landed, waiting for numbers from the reporter. Next:
phase 0 unless the log points elsewhere.

### Phase 2a — what changed and why

Reported on top of phase 2: opening a 38MB / 600k line log takes about half a
minute with a jumping scroll bar, Colorer slower than Chroma but both slow.

The scroll bar tracks `LineIndex.LineCount()`, so what the user watches for
those thirty seconds is the *indexer*, not the colours. Yet the choice of
highlighter changes the wait — which can only happen if the two are competing.
They are: `StartIndexing` publishes each batch of offsets through
`FrameManager.PostTask`, and the phase 2 walker ran its batches through the
same queue, on the same thread.

The walker sized a batch in lines: 200 normally, 2500 when it was still behind
the viewport. A line is not a unit of time. 2500 lines is a few milliseconds of
Chroma and around a hundred of Colorer, so on a file this size the queue spent
most of its time inside the highlighter and the indexer's batches waited their
turn. Fixed in three parts:

- a slice now runs until `hlSliceBudget` (4ms) of wall clock is up, checking
  the clock every `hlClockStride` lines. The stall is bounded whatever the
  highlighter costs per line, and a fast one gets more lines done per slice
  than the old fixed 200;
- the pause between slices is derived from the work just done, so the walker
  holds a duty cycle instead of a fixed sleep: `hlDutyVisible` when the
  viewport is still uncoloured, `hlDutyAhead` when it is walking past it, and
  `hlDutyIndexing` while the index is still being built. The index wins,
  because the scroll bar, `Ctrl+End` and position restore all wait for it;
- the schedule is computed inside the slice, on the UI thread. Phase 2 sampled
  `ScrollTopRow`, `lineStates` and `GetLogLineAtVisualRow` from the walker
  goroutine while the UI thread was writing them.

`EditorView.indexing` is the flag the duty cycle reads. `indexCancel` could not
serve: it is left non-nil after a normal finish, so it reads as "indexing
forever".

Also on the indexer side: the retry after `piecetable.ErrLoading` was a flat
20ms, which caps indexing at 50 chunk reads per second whenever the reader
outruns the loader. It now backs off from `indexPollMin` to `indexPollMax`.

### Phase 2a — what to measure next

Both loops now log a summary at `--debug`:

    EDITOR: Indexer stopped: N lines in T, W of it waiting for data, B UI batches
    EDITOR: Highlight walker stopped: N lines, U on the UI thread, T wall clock

That splits the wait three ways, and the next step depends on which one is
large:

- **W dominates** — the indexer is asleep on the async buffer, not scanning.
  The problem is in `async_buffer.go` / `piecetable.PieceTable.Read`, not here;
  pull those two files.
- **T minus W dominates with few batches** — the per-batch UI work is the cost.
  Suspects are `LineIndex.AppendOffsets` and `WrapEngine.InvalidateFrom`; pull
  `piecetable/lineindex.go` and `textlayout/wrap.go`.
- **the walker's U is comparable to the indexer's T** — the duty cycle is still
  too generous, or `Highlight` is being called for lines nobody will look at.
  That is phase 3, and phase 6 for the wasted attribute slices.

**Notes carried between sessions:**
- Word wrap is off by default, so `WrapEngine.ensureRowCountCache` is the cheap
  branch and is *not* part of this problem.
- Test file for the regression: `objdump -d install/far2l > far2l.s`, then F4,
  `Ctrl+End`.
- Quick check that the diagnosis still holds: set `EditorHighlighter = None`
  and press `Ctrl+End` — it should be instant.
- Second quick check, for the open-time report: `EditorHighlighter = None` and
  open the file. If it is still slow, nothing in this document is involved and
  the cost is in the loader or the line index.
- The slice budget and the three duty levels are constants in `editor_view.go`.
  If they turn out to need tuning per machine, they are the obvious candidates
  for `AppConfig` (principle 5), but do not add settings before the numbers ask
  for them.
