# Image and video support in f4 — plan and handover

This file is the entry point for the work on issue #186 (viewing pictures in
f4). It is written so that the work can be continued with nothing but the
repository at hand. Read it first, then, as needed:

- `TERMINAL.md` — the built-in terminal, including the kitty graphics protocol
  it accepts from child processes.
- `../vtui/GRAPHICS.md` — the graphics layer: `ImageSurface`, `ImagePlacement`,
  the protocol backends, cell size negotiation.
- `PLUGIN_PLAN.md` — unrelated work, but the same kind of document, and a good
  example of the shape this one is meant to keep.

## 1. How the work is done

The user is unxed, the author of f4. The conversation is in Russian; **all
code, comments and documentation are in English**. Testing happens on Linux
Mint 22.3 Cinnamon (X11) with `./run_all_tests.sh`; CI builds every OS,
including 32-bit ARM.

Rules, which have not changed since the plugin work:

- Every reply with code carries **tests** and an **English commit message**.
- Split a large task across several replies and start with a plan.
- Nothing from far2l copied verbatim: it is GPL2 and f4 is not.

The user values being told *why* a solution looks the way it does, being shown
the fork in the road and the reason one branch was taken, and being told what
was found along the way. Do not smooth things over.

## 2. Architecture

**One pipeline, several consumers.** `ImagePipe` (`image_pipeline.go`) is the
only thing that turns a path into pixels: it caches decoded surfaces, merges
concurrent requests for the same file, runs a small pool of workers, and
prefetches the neighbours of whatever is on screen. `PreviewSync` answers with
the best picture that can be had without decoding the whole file — a cached
one, a thumbnail seen earlier, or the thumbnail the file carries inside itself.
`LoadSync` decodes properly. Everything that shows a picture — the viewer, the
gallery, and eventually the quick view panel — asks the pipeline and never
touches a decoder directly.

**vtui ships rectangles of pixels, not pictures.** `ImageSurface` is straight
alpha RGBA; `ImagePlacement` says where on the cell grid it goes, which part of
the source to take, and at which z index. Backends (kitty today, iTerm2 and
sixel later, plus the native GUI renderers) know how to put that rectangle on
screen and nothing else. Anything the viewer wants to change about the picture
itself — a turn, a mirror — has to be baked into the pixels first. That is what
`image_transform.go` is for.

**The viewer is one frame with modes.** `ImageView` is a single frame that can
be showing one picture, the thumbnail grid, or a slide show. The grid and the
show are not separate frames because all three share the sibling list, the
pipeline, the graphics key and the set of picked pictures; separate frames
would need callbacks to keep four things in step and would gain nothing.

## 3. File map

In `f4` (package `main`):

- `image_pipeline.go` — the cache, the job queue, the workers, prefetch
- `image_decode.go`, `image_qoi.go`, `image_bmp.go`, `image_preview.go` —
  decoders and the Exif thumbnail path
- `image_transform.go` — rotation and mirroring over the RGBA bytes
- `image_view.go` — the viewer frame: layout, zoom, panning, orientation,
  the info overlay, keys
- `image_gallery.go` — the F12 thumbnail grid and the shared selection
- `image_slideshow.go` — the Ctrl+S timer
- `kitty_graphics.go` — accepting the kitty protocol in the built-in terminal
- `actions.go` — `tryOpenImageViewer`, `imageSiblingPaths`, and the wiring of
  the selection between the viewer and the panel
- `file_panel.go` — `ImageSiblings`, `SetSelectedByName`, `IsNameSelected`
- `config.go` — the `[Images]` section

In `vtui`:

- `graphics.go` — `ImageSurface`, `ImagePlacement`, the placement list
- `graphics_kitty.go` — the kitty output backend
- `graphics_scale.go` — `FitInside` and the scalers
- `framemanager.go` — `HideBars`, for a frame that wants the bar rows
- `terminal_env.go` — protocol detection

## 4. Status

Done before the current sequence of work: accepting the kitty protocol in the
built-in terminal (transmission, placement, drawing through
`vtui.GraphicsLayer`, cursor movement per the specification); pixel geometry in
`TIOCSWINSZ` and in the answers to `CSI 14/16 t`; announcing image support to
the child process (`KITTY_WINDOW_ID`, `TERM_PROGRAM`, `TERM=xterm-kitty` under
a terminfo check and the `AnnounceKittyTerm` option); the decoding pipeline;
preview from the Exif thumbnail; QOI and BMP decoders; walking the neighbours
with prefetch; the 100% mode; `Ctrl+R`; errors shown in the title.

Done in the current sequence, as steps 1 to 3 of the twelve below:

**1. Rotation and mirroring.** `image_transform.go` rotates and mirrors a
surface in plain Go. `ImageView` keeps `rotation`, `flipH`, `flipV` and a
`shown` surface; `display()` returns `shown` when the orientation has been
changed and the decoded `surface` when it has not, so an untouched picture is
never copied. Keys: `>` and `.` turn clockwise, `<` and `,` counter-clockwise,
`Alt+>` mirrors across the vertical axis, `Alt+<` across the horizontal one.

**2. Full screen and the info overlay.** `FrameManager.HideBars` in vtui;
`ImageView.ResizeConsole` remembers the console size and gives the key bar row
to the picture in full screen; `F` and `Ctrl+F` switch it, `Close` gives it
back. `Ctrl+I` (and `I`) raises a panel with the name, the size on screen, the
file size, the decoder, the scale and the orientation.

**3. The gallery and the slide show.** `F12` opens a grid of thumbnails;
`Ins` and `Del` pick and unpick, and the choice is shared with the panel in
both directions. `Ctrl+S` runs a slide show with the interval from
`[Images] SlideShowDelay`, five seconds by default.

**4. Quick view on `Ctrl+Q`.** In `quick_view_panel.go`, shows a picture through
the pipeline instead of the hex dump when one is under the cursor. The
placement is computed the way `ImageView.placementFor` does it, but inside the
bounds of the panel.

## 5. What is left, in order

**5. External decoder.** A fallback decoder at priority −10 that runs
`convert`, `magick` or `ffmpeg` — whichever is on `PATH` — and reads PNG from
its standard output, for `webp avif tiff heic jxl` and anything else. Register
it only when the binary exists. Extend the `[Images]` section with decoder
priorities by name and a timeout for the external call.

**6. Kitty polish in the built-in terminal.** Honour a negative `z` (picture
under the text) when drawing; save and restore placements across an alt-screen
switch; recompute on `Resize`; `t=s` (shared memory through `/dev/shm`);
unicode placeholders (`U=1` and the character `U+10EEEE`).

**7. iTerm2 and sixel output in vtui.** Add `GraphicsITerm2` (OSC 1337, base64
PNG) and `GraphicsSixel` (DCS, up to 256 palette colours, dithering) to
`graphics.go`; detect them from `TERM_PROGRAM=iTerm.app` and from a `CSI c`
answer containing 4. Test the shape of the sequences, not the pixels.

**8. Accepting iTerm2 and sixel in the built-in terminal.** Symmetrical to the
kitty side: OSC 1337 in `handleOSC`, sixel DCS in the parser, both feeding the
same placement layer.

**9. Fixing the kitty receiver in far2l.** In `far2l/src/vt/vtansi_kitty.cpp`:

- use `GetInt` rather than `GetChar` for `i` and `p` in `a=p` and `a=d`;
- apply `c`, `r`, `X`, `Y` and `z` when placing;
- `d=i` removes the placement but keeps the pixels, `d=I` frees the data;
- `a=d` with no `d`, and `d=a` / `d=A`, remove every visible placement;
- in `AddImage`, `if (rows > 0) { img.cols = cols; }` tests the wrong field —
  it has to test `cols`, and the missing side has to be computed from the
  aspect ratio;
- in `KittyArgs`, replace `if (i + 1 > j && s[j + 1] == '=')` with a test of
  `j + 1 < i`;
- answer `a=q` according to what the backend can actually do, through
  `GetConsoleImageCaps`.

**10. far2l to f4 detection.** far2l sets something like `FAR2L_IMAGES=1` for
its child when `GetConsoleImageCaps` reports RGBA support; take it into account
in `detectGraphicsProtocol` in `vtui/terminal_env.go` so that f4 running inside
far2l turns kitty on.

**11. far2l's own protocol in f4's built-in terminal.** Accept
`FARTTY_INTERACT_IMAGE_*` (see `far2l/WinPort/FarTTY.h`) in `HandleFar2lAPC`,
so that far2l running inside f4 can hand pictures over through its own channel.

**12. Video.** A second source of frames on top of the same placement layer:
decode through an external `ffmpeg` into a stream of RGBA, a frame timer, and
controls from the viewer (`Right`/`Left` for ±10 seconds, `Up`/`Down` for
volume).

Note that steps 9 to 11 exist because **kitty images do not work in either
direction between f4 and far2l today**: not when f4 runs inside far2l, and not
the other way round.

## 6. Decisions worth not undoing

- **The orientation is reset in `open()`, not in `SetImage()`.** `SetImage` is
  also called when the full resolution decode replaces the thumbnail of the
  *same* file. Resetting there would snap a picture back moments after the
  reader turned it.
- **A mirror reverses the direction of a turn.** The state is "rotate by
  `rotation`, then mirror", and for a reflection `R₉₀∘F = F∘R₋₉₀`. So when
  exactly one axis is mirrored, `Rotate` negates the delta; with both axes the
  mirror is a half turn, which commutes, and the sign stands.
- **`HideBars` has to live in the frame manager.** `ScreenObject.Show` forces
  an object visible, so `SetVisible(false)` on the key bar does not survive the
  next frame. The manager hides it — rather than merely skipping the drawing —
  because an invisible bar that still reports itself visible keeps swallowing
  clicks on the bottom row in `dispatchEvent`.
- **The overlay uses a negative z index.** In kitty, a `z` between −1073741824
  and −1 puts the picture under the glyphs but still over the cell background,
  which is what lets the info panel be readable without a box hiding the
  picture. Below −1073741824 the picture would go under the background too.
- **Thumbnails are fetched off the drawing path.** `PreviewSync` is cheap on a
  cached picture but reads the file header on one it has not seen; a screenful
  of tiles would otherwise mean a screenful of reads on every frame.
- **The slide show wraps around, `Step` does not.** Stopping at the ends of the
  directory makes it obvious where the directory ends; a show that stopped
  there would only be a slow way of pressing space.
- **`Stat` for the overlay happens once, lazily, and only when the overlay is
  actually up.** It can be a network round trip on a remote file system.

## 7. Traps

**`Ctrl+I` is Tab.** On a terminal without an extended keyboard protocol,
`Ctrl+I` arrives as `VK_TAB`, which the viewer uses for the 1:1 fit. Nothing in
f4 can tell them apart — the information is not in the event. Hence the plain
`I` alias. The same class of problem may affect `Alt+>` and `Alt+<`, which are
currently matched by `Char` with the Alt flag set; that has not been confirmed
on a real terminal.

## 8. Open questions and rough edges

None of these are bugs; they are places where a decision was made without
asking, and the user may want a different one.

- `Ctrl+R` goes through `open()` and therefore also resets the orientation.
  Arguably right for "read the file again", but it was not asked about.
- The gallery tile is 18 by 9 cells, chosen by eye for an 80x25 terminal. It
  could reasonably become a setting in `[Images]`.
- In the grid, the cursor colour wins over the picked colour, so a picked tile
  under the cursor only looks like the cursor. far2l marks the selection with a
  separate character; the grid could too.
- The slide show does not wait for a picture to finish decoding, so on a slow
  file system with a short interval frames will be skipped. A check on
  `iv.loading` in `slideStep` would fix it.
- `SlideShowDelay` is read and written but does not appear in the settings
  dialog.
- `vtui/framemanager_hidebars_test.go` calls `fm.renderPhase()` directly, which
  no test did before. If it turns out to be too heavy to call from a test,
  move the check one level down.
- `image_gallery_test.go` passes a nil context to `ImagePipe.LoadSync`, which
  the function handles but which no other test does.