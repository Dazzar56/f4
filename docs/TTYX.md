# TTY|Xi for f4 — the X server behind the terminal

A terminal is a stream of bytes, and a great deal of what a file manager wants
is not in that stream. It cannot tell `Ctrl+Shift+Up` from `Ctrl+Up` unless the
terminal implements a modern keyboard protocol. It cannot read the system
clipboard unless the terminal implements OSC 52 and is willing. It cannot show
a picture unless the terminal implements sixel or the kitty protocol. And it
cannot know where on the screen it is at all.

far2l answers this with **TTY|Xi**: a broker process holds a connection to the
X server and hands the terminal session back the things the TTY cannot carry.
`internal/ttyx` is the beginning of the same idea for f4.

Issues: [#662](https://github.com/unxed/f4/issues/662) (keys and clipboard),
[#663](https://github.com/unxed/f4/issues/663) (pictures over the terminal).

## 1. What exists

`internal/ttyx` — no cgo, and there will not be any. Everything goes through
XGB, which speaks the X protocol over a socket in plain Go and which vtui's own
X11 backend already depends on.

- `Session` — the connection, the terminal window, its geometry, its focus, and
  the event loop that keeps all three up to date.
- `Overlay` — an override-redirect window over the terminal that shows pixels,
  which follows the terminal when it moves and comes down when it loses the
  focus.
- `GrabKeys` — the key combinations a TTY cannot carry, taken from the X server
  and delivered as ordinary `vtinput` events.
- `image_x11_overlay.go` in `cmd/f4` — the image viewer's use of the overlay.

**The clipboard is not here, deliberately.** Issue #599 decided that all
clipboard work belongs in [goclip](https://github.com/unxed/goclip), and a
second implementation in f4 would be exactly the fragmentation that issue was
written to stop. What was found instead: goclip's native X11 driver did not
work at all — see section 6.

**What is left of #662** is the last hop: the key events reach `Session.Keys()`
but nothing in f4 reads that channel yet. See section 5.

## 2. Finding the terminal window

Three ways, best first. Which one succeeded is reported by `Session.Source()`,
and `Source.Trusted()` separates the two that identify a window from the one
that guesses.

1. **`$WINDOWID`** — xterm, urxvt, konsole and others publish their own window
   id for their children. Verified against the server before it is believed,
   because the variable outlives the window it names when a shell is passed
   along.
2. **`_NET_WM_PID`** — every window on `_NET_CLIENT_LIST` is asked which
   process owns it, and the answer is matched against our own ancestors, read
   out of `/proc`. This covers the terminals that publish nothing, including
   the ones where a single server process owns a window per tab: that process
   is an ancestor of the shell and therefore of us. When several tabs match,
   the focused one wins, because from outside the tabs are indistinguishable.
3. **`_NET_ACTIVE_WINDOW`** — whatever had the focus when we looked. Right most
   of the time and wrong exactly when the user was doing something else at the
   moment f4 started, which is why it is not trusted.

## 3. Showing a picture over the terminal

`ImageView` used to answer a terminal with no image protocol with *"This
backend cannot display images."* Under a local X session it now puts the
picture in an X window over the terminal instead.

The window is **override-redirect**, so the window manager neither decorates
it, nor gives it focus, nor lets the user drag it away from the terminal it
belongs to. Its **input region is emptied** through the SHAPE extension, so the
mouse passes through it and selecting text keeps working underneath a picture.
**Backing store** is asked for, because nothing here runs an event loop and
without it the server would expect a repaint on `Expose`.

The size of a character cell is not queried. It is the size of the terminal
window divided by the number of cells in it — exact when the terminal leaves no
padding, close enough when it does, and unlike `CSI 16 t` it needs no
cooperation from a terminal that has already shown it cooperates with nothing.

Two rules keep this from being a menace, and neither is optional:

- **The window has to have been identified, not guessed.** An
  override-redirect window is drawn over whatever is underneath it. Guessing
  wrong means painting over a stranger's application, so `SourceActive` stands
  down.
- **It comes down when the terminal loses the focus.** Nothing in X will take
  it down for us. The event loop does it; see section 4.

Switched off with `[Images] X11Overlay=0`. On by default, because it only ever
runs where the alternative is an apology.

## 4. The event loop

`Open` selects `FocusChange` and `StructureNotify` on the terminal window and
starts one goroutine that reads them. It is the only reader of the connection,
and it never holds the lock while it waits, so a request from another goroutine
is never stuck behind an event that has not arrived.

What it buys:

- `Focused()` and `Geometry()` are reads of cached values rather than round
  trips, so they can be asked on every frame.
- The overlay comes down the moment the terminal loses the focus and goes back
  up when it returns, without the caller having to notice. Nothing else in X
  will move an override-redirect window out of the way.
- The overlay moves with the terminal window while it is dragged.
- The key grabs are taken on focus and released on focus loss, which is not a
  nicety: a grab held by a window nobody is typing into takes that combination
  away from every other application on the desktop.
- `Changed()` carries a token whenever any of that changes, for an application
  that draws on demand and needs to know when to draw.

Two things learned the hard way here, each of which cost a hang or a crash:

- **A method that takes the lock must not be called from one that holds it.**
  `watch` asked `focusedNow` for the focus while holding the mutex, and the
  package deadlocked on the first connection it ever made.
- **The event goroutine must hold its own copy of the connection.** `Close`
  sets the field to nil, and a goroutine blocked in `WaitForEvent` would then
  read that nil rather than the connection it was waiting on. Closing the
  connection is what ends the wait, whatever the field says.

There is also a trap in XGB itself, worth knowing before adding another
extension: `shape.Init` and its siblings write two package level maps that
xgb's own reader goroutine reads for every event, without a lock. Initialising
an extension while any connection is live is therefore a data race inside the
library, and a concurrent map access in Go is a crash rather than a warning.
`registerShape` in `overlay.go` sidesteps it by registering only the
per-connection opcode, which is the only thing `shape.Rectangles` needs and
which lives in a map that does have a lock.

## 5. Known limits

- **A terminal with padding around its grid puts the picture out by a few
  pixels.** The division that finds the cell size cannot see the padding. `CSI
  16 t` would be exact where it is answered, and could be preferred when it is.
- **Only the viewer uses it.** Quick view, the gallery and the built-in
  terminal's own placements still go through `vtui.GraphicsLayer`. Wiring the
  overlay in as a `GraphicsProtocol` backend would cover all of them at once,
  and would belong in vtui.
- **The identification runs once, at the first picture.** A session that is
  detached and reattached elsewhere keeps pointing at the old window.

## 6. The clipboard lives in goclip, and did not work

Issue #599 says the clipboard is goclip's job and that anything missing there
gets fixed there. Measuring it first turned out to be the whole story: goclip's
pure-Go X11 driver created its window as `InputOnly` with the root depth, and
an `InputOnly` window must have depth zero and the `CopyFromParent` visual.
Every call therefore failed with `BadMatch` on `CreateWindow`, the native path
never ran once on any server, and every copy and paste fell through to `xclip`
— which #599 opens by pointing out is not installed by default anywhere.

Three more defects were behind it, none of which could show while the first one
stood:

- **No `INCR`.** An owner answering a large paste sends a handshake and then the
  value in pieces. goclip read the handshake and returned it as if it were the
  text, so a paste from a browser or an office suite produced four bytes of
  binary rather than the document.
- **`BytesAfter` ignored.** Even a value that came whole was truncated at
  whatever the server chose to put in the first reply.
- **Two readers of one event stream.** `ReadText` polled for events while the
  serving goroutine was blocked in `WaitForEvent` on the same connection, so
  either could swallow what the other was waiting for.

All four are fixed in the goclip patch that goes with this, along with
outgoing `INCR`, so a large copy works in both directions. Eight tests run it
against a real server.

## 7. What #662 still needs

One hop. `Session.GrabKeys` takes the combinations and `Session.Keys()`
delivers them as `vtinput.InputEvent` values, translated by `keytrans` — the
same translator vtui's X11 backend uses, so a key arriving this way is
indistinguishable from one arriving in GUI mode. What is missing is that
nothing in f4 reads that channel: the events would have to be merged into the
input stream `vtui.FrameManager` dispatches from, and vtui has no exported way
to inject one. A small hook there — the equivalent of the `dispatchEvent` it
already has internally — and the rest of #662 is wiring.

Which combinations to ask for is the other open question, and it is a policy
question rather than a technical one. A blanket grab is antisocial; the honest
set is the combinations f4 has bindings for and the TTY cannot deliver.

## 8. Testing

`internal/ttyx` tests run against a real X server, which on a machine without
one is no server at all:

```sh
Xvfb :99 -screen 0 1280x800x24 &
DISPLAY=:99 go test ./internal/ttyx/
```

Without `$DISPLAY` they skip. The overlay test writes a gradient, reads it back
with `GetImage` and compares it pixel by pixel, so a channel swap fails it
rather than producing a plausible wrong colour. The pure arithmetic — the
ancestor walk, the window id parsing, the cell to pixel conversion — is tested
without a server.
