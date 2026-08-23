# f4 Terminal Architecture Manifest

The built-in terminal in `f4` is one of its most complex components. This document serves as a comprehensive guide for human developers and AI assistants. It explains the fundamental challenges of cross-platform terminal emulation (specifically Windows ConPTY), analyzes how industry-leading terminal emulators solve them, and justifies the final architectural design chosen for `f4`.

## 1. The Fundamental Conflict: Streams vs. Grids

To build a terminal that supports an infinite scrollback history (like `Ctrl+O` in Far Manager) while correctly displaying interactive applications, we must bridge two completely different OS philosophies:

*   **POSIX (Linux/macOS):** The terminal is fundamentally a **stream of bytes** (`stdout`). The OS sends continuous text. If the text hits the right edge of the window, the terminal emulator (not the OS) decides to wrap it to the next line (Soft Wrap). Applications like `nano` explicitly request to switch to an "Alternate Screen Buffer" to draw 2D interfaces, keeping the main stream clean.
*   **Windows Console API:** The terminal is fundamentally a **2D Grid of cells** (managed by `conhost.exe`). When a program like `cmd.exe` runs a simple `dir` command, it doesn't just output a stream of text. It asks the OS to draw text at specific X/Y coordinates in that 2D grid. 

## 2. The ConPTY Challenge

To support modern Windows features without relying on legacy APIs, `f4` uses **ConPTY** (Windows Pseudo Console). 

ConPTY acts as a "Screen Scraper". It maintains an invisible 2D grid in the background. When `cmd.exe` writes to the console, ConPTY calculates the difference between the old 2D grid and the new one, and generates ANSI escape sequences (Cursor Up, Cursor Down, Erase Line) to replicate this difference.

**The Quirk:** Because `cmd.exe` draws its prompt and output using explicit coordinates, ConPTY emits absolute cursor positioning codes (e.g., `\e[10;1H`) even for simple sequential output. It does not output a continuous flat log.

## 3. Industry Standards: How Others Do It

Before finalizing the `f4` architecture, we analyzed the source code of the most popular terminal emulators (as of 2024/2025):

### A. The "Honest" VTE with Active Reflow (Alacritty, WezTerm, Rio)
*   **How it works:** The entire scrollback history is stored as a massive deque of `Line` or `Cell` objects (`VecDeque<Line>`). Each line holds an `is_wrapped` flag. On window resize, the entire history is re-iterated, wrapped lines are joined, and re-sliced to the new width.
*   **ConPTY Handling:** They use complex heuristics (`enable_conpty_quirks` in WezTerm) on every printed character to guess if ConPTY accidentally inserted a hard break instead of a soft wrap. On Windows, they often pad the screen with empty lines during resize to prevent the cursor from jumping into the scrollback history.

### B. The Web-Based Integrators (Hyper, Tabby, Fluent Terminal)
*   **How it works:** They rely entirely on `node-pty` (backend) and `xterm.js` (frontend). The architecture is fundamentally a bridge passing data chunks between processes.
*   **ConPTY Handling:** Delegated to `xterm.js`, which handles it like a standard 2D grid. Fluent Terminal uses "Resize Throttling" (blocking output during resize) to prevent ConPTY artifacting.

### C. The API Translation Layer (far2l)
*   **How it works:** Parses ANSI codes from the PTY and translates them back into native Windows-like Console API calls (`WINPORT(WriteConsole)`). Scrollback is achieved by intercepting the API's scroll events via a callback.
*   **Why it wasn't chosen:** `f4` aims to render entirely via ANSI escape sequences (`vtui`) to support modern terminals (Kitty, iTerm2). Emulating WinAPI in Go is non-idiomatic and creates unnecessary friction.

## 4. The f4 Decision: VTE Mirror (Grid Extrusion)

*Rule of thumb: Deviating from battle-tested mainstream solutions (like Alacritty's Active Reflow) requires concrete arguments.*

**We explicitly chose NOT to use the Active Reflow architecture (storing history as a `Grid` of `Cell` objects).** Instead, `f4` uses a custom architecture called **VTE Mirror**.

### How VTE Mirror Works (Updated with GridHistory):
1.  **The Active Grid (Viewport):** The terminal maintains a 2D array of cells (`[][]vtui.CharInfo`).
2.  **Horizontal Preservation:** Unlike standard terminals, `f4` allows rows in the Active Grid to temporarily exceed the viewport width. If the terminal is resized horizontally (e.g., shrunk), the off-screen characters are preserved in memory. When the window is enlarged again, the characters reappear. Commands like `EraseLine` respect this preserved length to prevent ghost artifacts.
3.  **Soft Wrap Detection:** If text hits the right edge of the viewport, the `WrapFlags[y]` boolean is set to `true` for that row.
4.  **GridHistory (The Staging Area):** When a line is pushed off the top of the Active Grid (due to scrolling or vertical shrink), it is not immediately serialized. Instead, it enters a `GridHistory` buffer (up to 2000 lines). This allows instant, lossless restoration of the screen when the terminal window is enlarged vertically.
5.  **Extrusion (Linearization):** When the `GridHistory` overflows, the oldest lines are stripped of trailing spaces and permanently serialized into `f4`'s internal `PieceTable` byte stream.
6.  **Reconstruction:** To view the log (`Ctrl+O`), `f4` simply concatenates: `PieceTable` + `GridHistory` + `ActiveGrid`.

### Concrete Arguments for VTE Mirror:
1.  **Domain-Specific Optimization:** `f4` is a file manager, not just a terminal emulator. It already possesses a highly optimized, zero-allocation `PieceTable` engine used for its Editor and Viewer. Extruding the terminal log directly into a `PieceTable` allows the internal Viewer (`F3`) to open a 10-gigabyte terminal log instantly without allocating memory for millions of `Cell` structs.
2.  **No Active Reflow (Preventing Shell Desync):** Unlike Alacritty or Windows Terminal, `f4` intentionally avoids reflowing (re-wrapping) the live 2D grid upon window resize. Shells (Bash, PowerShell) track the cursor natively and redraw their prompt upon receiving a `WINCH` (Window Change) signal. If the emulator reflows the grid under the shell's feet, cursor coordinates desync, causing severe visual corruption (e.g., typing overwriting previous lines). Instead, `f4` uses **Horizontal Preservation** for the live grid (hiding off-screen characters without deleting them) and relies on the `PieceTable`'s `WrapEngine` to perform mathematically perfect reflow when the user views the scrollback history (e.g., via `Ctrl+O`).
3.  **ConPTY Isolation:** ConPTY frequently forces full screen redraws. The VTE Mirror restricts ConPTY's chaos to a fixed-size sandbox (the viewport). The permanent log is immune to cursor-jumping artifacts because lines are only saved when they are mathematically guaranteed to be finished (pushed off the top).
3.  **Golang GC Efficiency:** Go's Garbage Collector handles large contiguous byte slices (`PieceTable` chunks) orders of magnitude better than deep hierarchies of small, pointer-heavy objects (`[]Cell` for infinite scrollback).

## 5. Critical Implementation Rules (Lessons Learned)

Do not attempt to "optimize" or change the following behaviors without consulting this list. They were discovered through painful debugging of Windows ConPTY.

*   **Rule 1: The PTY Drop Order Deadlock.** 
    When shutting down a terminal on Windows, **the ConPTY handle MUST be closed BEFORE the stdin/stdout pipes are closed.** Failing to do this causes a process deadlock (observed in Alacritty, Rio, and `node-pty`).
*   **Rule 2: Bottom-Aligned Initialization.**
    A visual terminal inside a file manager must always initialize with its cursor at `(0, height-1)`. This ensures that incoming text visually "sticks" to the bottom of the screen (like standard `cmd` or `bash` behavior) instead of floating at the top and leaving empty space below. The `GridHistory` buffer cleanly absorbs any "floating UI" glitches during resizing.
*   **Rule 3: Extrusion Guard (The 23 Empty Lines Bug).** 
    When a shell starts, it often issues a `clear` command (`\e[2J`), causing the terminal to scroll. If the log is empty, **do not** extrude empty lines into the `PieceTable`. This prevents the log from starting with 23 blank lines.
*   **Rule 4: Ignore 0x0 Resizes.** 
    If the window is minimized, Windows may send a resize event for `0x0`. Passing this to ConPTY will crash it or corrupt its internal state.

## 6. Inspiring Features for Future Implementation

Our analysis of competitor codebases revealed several brilliant UX and performance solutions that we intend to implement in `f4`:

1.  **Resize Throttling & Output Blocking (from Fluent Terminal):** 
    During a rapid window resize, ConPTY gets confused and generates garbage. We should buffer incoming PTY data in a `MemoryStream` while the user is dragging the window border, and only flush it to the parser ~60ms after the resize stops.
2.  **Smart Scroll Pinning (from Tabby):** 
    If the user scrolls up the history (`Ctrl+O` in Far), we should set a `pinnedToBottom = false` flag. New output from the PTY should quietly append to the `PieceTable` in the background without forcing the user's view to snap back to the bottom.
3.  **UAC Bridge via Named Pipes (from Tabby):**
    To support opening an "Admin Tab" within the standard user's `f4` window, we can spawn a small elevated helper process (`UAC.exe`). This helper creates the ConPTY session and pipes the data back to the main `f4` process via Windows Named Pipes.
4.  **SPSC / Async PTY IO (from Rio):**
    Reading from anonymous pipes on Windows is synchronous. The `Pty.Read()` call MUST be isolated in its own dedicated goroutine, communicating with the main `FrameManager` via channels (or lock-free queues) to prevent UI freezes during heavy output (`cat /dev/urandom`).

## Appendix A: The ConPTY Observation Log

During the development of `f4`, the following intrinsic behaviors of the Windows ConPTY engine were observed. These are not bugs in `f4`, but fundamental properties of the "Screen Scraper" architecture used by Windows:

1.  **The Echo Chamber:** Every byte sent to the ConPTY input pipe is echoed back to the output pipe as a separate data chunk. This includes long technical strings used for synchronization.
2.  **Absolute Grid Obsession:** Even for simple sequential text output, ConPTY frequently issues absolute cursor positioning commands (e.g., `\x1b[H` to home or `\x1b[row;colH`). It treats the terminal strictly as a 2D grid, not a stream.
3.  **The Premature Prompt:** When a shell command (via `cmd` or `powershell`) finishes, ConPTY renders the next command prompt and moves the cursor *immediately*. This often happens *before* the application can process any trailing escape sequences or control codes sent at the end of the command string.
4.  **Implicit Clears:** ConPTY may unilaterally issue a "Clear Screen" (`\x1b[2J`) or "Erase Line" (`\x1b[K`) sequence during initialization or window resizing, even if the application did not explicitly request one.
5.  **Coordinate Jump Scares:** After an `Erase Display` command, ConPTY almost always forces the cursor back to `(1,1)` (Top-Left), regardless of where the cursor was previously or where the TUI expects it to be.
6.  **Chunk Fragmentation:** Large blocks of output are often fragmented into small, seemingly random chunks, where ANSI escape sequences are split across multiple `Read` operations.

### Current Blockers / Open Issues (Windows specific)

~~1.  **Technical Echo Leakage (Refers to Observation 1):**~~ **SOLVED.** We eliminated the need for `powershell` wrappers entirely by using the `$E` variable in the `PROMPT` environment to automatically emit OSC 133 sequences.
~~2.  **The Duplicate Prompt Problem (Refers to Observation 3):**~~ **SOLVED.** We no longer suppress the native shell prompt. We embrace it, allowing it to act as the true command history delimiter, perfectly mimicking Far Manager.
~~3.  **Bottom-Alignment Defeat (Refers to Observations 2 & 5):**~~ **SOLVED.** Implemented "Visual Gravity" in the render pipeline (`Show()`). The active viewport is dynamically shifted downwards, guaranteeing bottom-alignment regardless of ConPTY's absolute coordinate positioning.
## 7. Host Console Mode (`ShellModeHost`)

In addition to the default built-in terminal emulator (`ShellModeOwn`), `f4` provides an optional **Host Console Mode** (`Panel.ConsoleMode = host` in `settings.ini` or configured in Panel Settings):

### 1. Architecture: PTY Passthrough + Silent VTE Mirror
* **Direct Output Passthrough:** When panels are toggled off (`Ctrl+O`) or a shell command runs, PTY bytes are forwarded byte-for-byte to the real host terminal's primary screen buffer (`vtui.WritePassthrough`).
* **Silent VTE Mirror:** PTY output is simultaneously fed to the internal `AnsiParser` using `mutedPTY`. The mirror maintains full state (PieceTable log, alt screen state, mouse tracking modes, kitty flags) without generating duplicate terminal responses to queries (CPR, DSR, DA, OSC 52).
* **Native Terminal Capabilities:** The host terminal provides true native scrollback, mouse selection, native sixel/kitty graphics, and native job control (`Ctrl+C`, `Ctrl+Z`, `SIGTSTP`).
* **Far-Style Overlay (`ConsoleOverlayUI = true`):** When enabled, `f4` reserves 1–2 lines at the bottom using a `DECSTBM` scroll region (`\x1b[1;<h-n>r`), rendering its command line and keybar directly onto the host console without touching `ScreenBuf`.

### 2. Graceful Degradation Modes
* **`ShellModeSimpleInline`:** Used when PTY/ConPTY is unavailable on Windows (e.g. running under Wine in console). Runs commands with inherited `stdio` via `vtui.Suspend()` / `exec` / `vtui.Resume()` and a keypress pause.
* **`ShellModeSimpleCaptured`:** Used in GUI and non-TTY environments when PTY is unavailable. Runs commands via `LocalCommandRunner` and streams output into an `f4` dialog.

## 8. Graphics in the Built-in Terminal

The built-in terminal accepts pictures from the program running inside it. What
arrives becomes a *placement*: a rectangle of pixels anchored to a row of the
grid, which scrolls with the text around it, follows a resize, is clipped at
the edges of the terminal, and disappears when the row it was printed next to
is gone. `kitty_placements.go` owns that list and every protocol feeds it.

### 1. What is accepted

| Sequence | Protocol | Receiver |
| :--- | :--- | :--- |
| `ESC _ G ... ESC \` | kitty graphics | `kitty_graphics.go` |
| `ESC P ... q ... ESC \` | sixel | `sixel_decode.go` |
| `ESC _ far2l... ESC \` | far2l interact | `HandleFar2lAPC` (images not yet) |

Any other device control string is parsed and dropped. Before sixel support
there were no DCS states at all: `ESC P` was swallowed by `handleEsc` and the
body of the sequence then printed itself onto the screen as text.

### 2. What is answered

A program decides whether to send pictures by asking, so the answers matter as
much as the receivers:

*   **`CSI c` (DA1)** → `CSI ? 62 ; 4 c`. The `4` is what advertises sixel; no
    client will send a picture without it. The level rose from the old `1;2`
    at the same time, because sixel does not exist below VT220.
*   **`CSI 14 t` / `CSI 16 t` / `CSI 18 t`** → the text area in pixels, the
    cell in pixels, the screen in cells.
*   **`CSI ? Pi ; Pa ; Pv S` (XTSMGRAPHICS)** → 256 colour registers, and the
    sixel raster geometry. Some libraries block on this answer.
*   **`CSI ? 80 h` / `l` (DECSDM)** → sixel scrolling off / on. Set disables
    scrolling, which is the way round the hardware behaved and the way round
    xterm settled on after years of the opposite. Reset is the default.

### 3. Where the cursor ends up after a sixel image

At the sixel active position: the column the picture started in, and the row
the sixel cursor reached. A dump ending in a graphics new line (`-`) therefore
leaves the next line of text below the picture; a dump ending in data leaves it
**on top of** the picture.

This is Windows Terminal's rule, chosen deliberately. It looks like a defect
and is regularly reported as one, but it is the only rule under which a program
can print an image on the bottom row at all: anything that moves the cursor
past the picture scrolls a line away to make room for a cursor the client never
asked to move. xterm and mlterm each invented their own algorithm and do not
agree with each other either. A client that wants its text below the image
sends a line feed.

### 4. Known deviations and defects

Read this list before concluding that something is broken.

*   **Text does not punch a hole in a picture; the picture covers the text.**
    On the hardware, and in Windows Terminal, the screen is one bitmap and
    writing a character clears the cell it lands in, so text output over an
    image erases that part of the image. Here a placement is drawn *over* the
    cells at z index 0, so the text goes under it instead. Combined with the
    cursor rule above, a shell prompt that lands on the last row of an image is
    invisible rather than punched through. Fixing it properly means either a
    negative z index, which vtui cannot express yet (see the entry in
    `IMAGES_PLAN.md` section 8), or per-row image slices of the kind Windows
    Terminal keeps.
*   **No byte of child output is ever held back, and that shapes a heuristic.**
    `exciseWindowsSync` hides the background `cd` command that keeps the panel
    and the shell in step. It used to survive a chunk boundary by withholding
    any tail of the data that could begin `cd /d "` — which a lone `c` can, so
    a chunk ending on the `c` of `CSI c` was never answered and every sixel
    client, all of which open with that query, timed out and decided the
    terminal had no graphics. Those bytes are now printed and only remembered
    for matching; when the command does complete, the erase-line the excision
    already writes takes the fragment off the screen with the rest of it. A
    fragment can therefore be visible for one frame, which is the price. The
    rule to keep: **the parser may defer a byte only once it has seen the whole
    seven byte marker, never on a guess.**
*   **A DECRQSS query gets silence.** It used to get its own text printed onto
    the screen, which was worse, but neither is an answer.
*   **The cell size is the real one, not a virtual 10x20.** Windows Terminal
    rasterises sixel into a fixed cell to emulate a VT340. We cannot: `CSI 16 t`
    already tells the child what our cell is, and it has to agree with what
    vtui's sixel encoder assumes, or f4 running inside f4 would draw itself at
    the wrong size.
*   **One sequence, one picture.** Sixel on the hardware writes into a screen
    wide bitmap that later sequences keep drawing into. Here each sequence
    produces its own placement. Paging (`DECGRA`), rectangular copy between
    pages, and a palette shared across images are all consequences of the
    bitmap model and none of them are implemented.
*   **Rule 2 of section 5 applies to the cursor as well.** A fresh terminal
    starts with the cursor at `(0, height-1)`, so a picture printed before
    anything else is placed on the bottom row and scrolls the screen.
