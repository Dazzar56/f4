# Video in f4

Issue [#186](https://github.com/unxed/f4/issues/186), step 12. F3 on a video
file plays it in a window over the terminal, the same window the picture viewer
draws into: it is placed where the frame is, it follows the terminal when the
terminal moves, it comes down when the terminal loses the focus, and it goes
when the frame is closed.

## 1. Local only, and not as a compromise

Video does not go down a terminal. Twenty five frames of 976x748 RGBA is 730
MB/s; sixel takes a tenth of that and spends a core doing it; the only terminal
protocol with a chance is kitty's shared memory, which is local by definition.

So the question is not whether to support playing over ssh — it is that there
is nothing to support. Video is a local X affair, which is why it is built on
`internal/ttyx` and why `tryOpenVideoPlayer` refuses without a session.

## 2. mpv now, ffmpeg next

This is the short way round, and it is deliberate.

The long way — decode with ffmpeg into a stream of RGBA and push the frames
into the overlay ourselves — ends with f4 owning the timing, the seeking and
the audio, and it is the one that has to exist before anybody can watch
anything. So mpv goes in first: it is a day's work rather than a week's and it
puts a working player in front of people while the other one is written.

What makes it more than "f4 launches a player" is `--wid`. mpv draws into f4's
own X window, so everything the overlay already does applies: position,
following, hiding, cleanup. A player launched on its own would float over the
wrong application the moment the user switched away, and outlive f4 if f4 died.

The window has no keyboard — it is override-redirect and shaped out of the
input region, which is what lets the terminal underneath keep working — so mpv
is driven down its IPC socket instead of through its own bindings. Space pauses,
the arrows seek ten seconds and move the volume, Esc and F10 close.

## 3. The focus rule

Playback carries on while the terminal is not on top, the way a player behaves.
The picture goes with the terminal, because an override-redirect window that
stayed up would be over somebody else's application, but the film keeps
running and the sound keeps coming.

`[Video] PauseOnFocusLoss=1` for whoever wants the other behaviour.

## 4. Missing tools

A file manager that answers "cannot play this" is answering the wrong question.
`external_tools.go` finds a tool under any of the names it goes by — ffmpeg is
also `avconv`, the Libav fork, which some distributions still ship — and when
it is missing it says what is missing, what it was for, and the command that
installs it, chosen from the package manager that is actually on the machine
rather than guessed from the operating system.

## 5. What is left

- **The ffmpeg path.** Decode to RGBA through a pipe, a frame timer, the frames
  pushed into the overlay directly rather than through the placement layer —
  a placement is re-sent whole whenever it changes, which at twenty five frames
  a second is the wrong shape entirely. Audio through a second process, synced
  on PTS rather than on a frame count.
- **A position bar and a duration.** mpv knows both and will answer down the
  same socket; nothing asks yet.
- **Windows.** The overlay is X only, so this is too. A console window on
  Windows can be found and drawn over the same way, which is how Far's picture
  viewer worked, but none of that machinery exists here yet.
