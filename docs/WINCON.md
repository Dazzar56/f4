# Pictures over a Windows console

`internal/wincon` puts a picture in a window over the console, for a console
that has no way of showing one itself.

## 1. conhost only, and on purpose

**Windows Terminal is not this.** It renders sixel, so pictures go down the
wire the way they do on any capable terminal — that is what the full colour
sixel encoder in vtui is for, and it is a better answer than an overlay in
every respect.

This is for **conhost**: `cmd.exe` in its own window, which has no image
protocol of any kind and never will.

The two are told apart by `GetConsoleWindow`. On conhost it returns the real,
visible window. Under Windows Terminal the console lives in a pseudoconsole
whose window exists and is never shown, so `IsWindowVisible` is false and
`Source.Trusted()` says no. Drawing over a window nobody sees would put the
pictures nowhere.

## 2. A child of the console window

Same decision as the X side and for the same reasons: the overlay is a child
window of the console's, not a window over the top of everything.

- it moves with the console, because the system moves children;
- it is clipped to the console's client area;
- anything raised above the console is above it too;
- it is not top-level, so no task list and no alt-tab ever offers it;
- it is destroyed with its parent.

`WM_NCHITTEST` answers `HTTRANSPARENT`, so the mouse goes to the console
underneath and selecting text keeps working — the same rule as the empty input
region on the X side. `WM_ERASEBKGND` is answered so the background never
flashes before a picture lands.

**One caveat is real.** The console window belongs to `conhost.exe`, a
different process, so parenting to it attaches the two threads' input queues.
That is how every overlay onto a console works, and it is what makes the
picture behave; the price is that a wedged conhost can wedge the thread that
pumps the overlay.

**So nothing else in f4 waits on that thread.** This is an invariant, not an
observation, and issue #805 is what breaking it looked like: `Place`, `Hide`
and `SetBounds` used to call `SetWindowPos`, `ShowWindow` and `SetWindowRgn`
from whichever thread called them, and those three are *synchronous* on a
window owned by another thread — they send messages to the owner and wait for
it. The caller was `RenderExternal`, which runs with the vtui screen locked, so
one frame of f4 ended up waiting on the pump thread, the pump thread shares an
input queue with conhost, and conhost is what draws f4's own text and delivers
its keys. From the outside: a black rectangle where the picture should be, the
whole interface stopped, and `Esc` answered when Windows broke the wait a
couple of minutes later.

The invariant is kept by splitting the overlay in two. `overlay_state.go` is
what the window *should* look like — position, region, whether the frame buffer
has changed, and one flag that keeps at most one wake-up outstanding no matter
how many changes arrive. Callers write there and post a single `WM_APP+2` with
`PostThreadMessageW`, which does not wait and does not route the wake-up through
the cross-process child HWND. The pump thread answers that message and makes
every window call there is. The rule to keep: **`user32` and `gdi32`
calls that touch the overlay window live in `wndProc`, `paint` and `apply`, and
nowhere else.** `GetClientRect` on the parent is the one exception and is safe:
it reads window data and sends nothing.

**And the window is never shown or moved except in the wake-up that paints
it.** The report had two halves and the threading is only the first of them:
`WM_ERASEBKGND` is answered so the background never flashes, which also means
an unpainted window shows whatever it last held — black, the first time. A
frame places the window, then reshapes it, then hands over the pixels, so the
pump thread could show an empty window and keep it there for as long as
scaling a photograph takes. `take` therefore holds a move back until the frame
buffer has been replaced; the `Draw` that follows is at most one wake-up away,
and the two then happen together. The same rule covers a resize, because
`paint` blits the frame buffer at its own size and leaves the rest of a larger
window alone.

`New` bounds its own wait too. Creating the window is the call that performs
the attach, so it is the one place at startup a wedged conhost could hold f4
up; after five seconds f4 goes on without an overlay, which is a perfectly good
outcome.

## 3. The geometry is the easy part here

Unlike a terminal emulator, a console answers directly:

- `GetCurrentConsoleFontEx` gives the pixel size of a character cell. Nothing
  to infer, nothing to round, no escape sequence for somebody else to swallow.
- `GetClientRect` gives the text area, and the text starts at its corner —
  there is no menu bar inside a console's client area and no logical-pixel
  scaling to undo.

So the whole of `docs/TTYX.md` section 3, which is about working out where the
grid is, has no counterpart here. That part of the X side is guesswork forced
by terminals that do not answer; Windows answers.

## 4. What is tested and what is not

`geometry.go` is arithmetic and policy, has no system calls in it, and is
tested on every platform: cell rectangles, clipping, the union that decides
which window one frame goes into, and the composing into a device independent
bitmap — which is bottom-up with its channels the other way round, and both
mistakes look like a picture rather than an error. Composing and not copying:
a picture can arrive as overlapping pieces, which is what a stack of
transparent sixel layers from a program in the built-in terminal is, and a
copy leaves the top layer alone on the screen.

`overlay_state.go` is the same kind of file as `geometry.go`: what was asked
for, what is on the screen, and what the pump thread therefore has to do, with
no system calls in it. It is tested everywhere — coalescing, a change arriving
while the pump thread is busy, placing twice in the same spot, hiding, showing
again, clearing the region, refusing to record anything once closed, and the
move that waits for its pixels while the region and the repaint do not.

`overlay_windows.go` is the part that calls `user32` and `gdi32`. It compiles
for `windows/amd64` and `windows/arm64`, the shape of it is the same as the X
overlay, and the only report from a real console so far is issue #805.

When a picture does not appear, `VTUI_DEBUG=1` gives one `WINCON:` line per
frame that changed: the size and corner of the window and how many pictures
went into it. No lines means the frame never reached the overlay; lines with a
black rectangle on the screen means the request reached the pump thread and
something below it went wrong. `[Images] Overlay=0` turns the overlay off
altogether — the same setting serves both platforms, and `X11Overlay` is the
name it had when the X side was the only one — which is how to tell an overlay
fault from anything else.

Beside them is a summary line, at most one a second and only for a second in
which something happened:

    WINCON: 1.0s frames=57 new=2 scale=812ms/406ms window=3ms \
            pump=6 move=2 rgn=2 inval=4 paint=4 blank=1 gaveup=0

`frames` is how many times a frame reached the overlay and `new` how many of
them were not the frame before, so `frames` high with `new` low is a console
redrawing under a still picture and costs nothing, while the two rising
together is a picture being rescaled sixty times a second. `scale` is the
total and then the worst single scale of the period, on the thread that holds
the screen lock: a camera JPEG is tens of megapixels and the resampler is
plain Go, so a frozen f4 with a large number here is frozen *there* and
nowhere near a window call. `paint` and `blank` come from the pump thread —
`blank` counts the paints that found no frame buffer, which is what a black
rectangle looks like from inside. `gaveup` names the reason the last frame
was abandoned.

The pump thread counts rather than logs, deliberately: its input queue is
attached to conhost's, so a write to a file on that thread is one more way of
stopping the console it is drawing over. See `internal/wincon/stats.go`.
