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

- `Session` — the connection, the terminal window, its geometry and its focus.
- `Overlay` — an override-redirect window over the terminal that shows pixels.
- `image_x11_overlay.go` in `cmd/f4` — the image viewer's use of both.

**Not done:** the keys and the clipboard of #662. The connection and the window
identity they need are here; the grabbing itself is not, and belongs in
`vtinput` rather than in f4. See section 5.

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
  it down for us. `Session.Focused()` is checked on every frame the viewer
  draws.

Switched off with `[Images] X11Overlay=0`. On by default, because it only ever
runs where the alternative is an apology.

## 4. Known limits

- **The focus is only checked when the viewer draws.** Alt-tab away from a
  terminal showing a picture and the picture stays up until something makes f4
  redraw. A proper fix is an event loop on the X connection — `FocusOut` on the
  terminal window — which is also what #662 needs, so it is worth doing once
  and for both.
- **The overlay does not follow the window while it moves.** Same reason: the
  geometry is read when the frame is drawn, not when `ConfigureNotify` says it
  changed.
- **A terminal with padding around its grid puts the picture out by a few
  pixels.** The division that finds the cell size cannot see the padding. `CSI
  16 t` would be exact where it is answered, and could be preferred when it is.
- **Only the viewer uses it.** Quick view, the gallery and the built-in
  terminal's own placements still go through `vtui.GraphicsLayer`. Wiring the
  overlay in as a `GraphicsProtocol` backend would cover all of them at once,
  and would belong in vtui.
- **The identification runs once, at the first picture.** A session that is
  detached and reattached elsewhere keeps pointing at the old window.

## 5. What #662 still needs

The keyboard half is the larger one and it belongs in `vtinput`, next to the
kitty keyboard protocol and Win32 input mode it would sit beside:

1. **Reading the keyboard.** `XGrabKey` on the combinations the terminal
   swallows, or an `XInput2` passive grab on the terminal window, translated
   into `vtinput.InputEvent` through `keytrans`, which already talks to X for
   layouts. Grabs are shared state on the X server: whatever is taken has to be
   released the moment the terminal loses the focus, or the rest of the desktop
   loses those keys.
2. **The clipboard.** X selections through the same connection, which removes
   the OSC 52 round trip and the terminals that refuse it. `PRIMARY` and
   `CLIPBOARD`, `INCR` for anything large.
3. **The event loop.** Both of the above need one, and so do the two limits in
   section 4. It should live in the session and deliver focus changes,
   geometry changes and key events on channels.

The order matters: the event loop first, because everything else is built on
it, and because it turns the two known limits of the overlay into fixed ones.

## 6. Testing

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
