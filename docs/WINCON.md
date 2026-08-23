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
pumps the overlay. It is a thread of its own for exactly that reason, and
nothing else in f4 waits on it.

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
which window one frame goes into, and the copy into a device independent
bitmap — which is bottom-up with its channels the other way round, and both
mistakes look like a picture rather than an error.

`overlay_windows.go` is the part that calls `user32` and `gdi32`. **It has not
been run on Windows.** It compiles for `windows/amd64` and `windows/arm64` and
the shape of it is the same as the X overlay, which has been run, but the first
report from a real console is the first real test.
