# Review queue

Doubts, shortcuts and things deliberately left alone, written down here so
they can be reviewed in one sitting instead of being rediscovered one at a
time. Nothing here blocks anything; anything that grows a plan of its own
moves out of this file.

## Positions are keyed by the bare path

`file_state.go` stores the last editor and viewer position under the path
string alone. A path is unique only within one file system, so a remote path
and a local one collide, and so do two remote hosts with the same layout.
Step 13b in `FISH+.md`.

## AsyncBuffer can hand out a short read without saying so

`AsyncBuffer.Read` clips what it copies to the length of the chunk it found
and returns no error when the result is shorter than asked for. A chunk is
stored short whenever the underlying `ReadAt` returned fewer bytes together
with `io.EOF`, which for a remote file is not only the end of the file.
Anything that advances by `len(data)` then loses its alignment silently, and
the line offsets it records are wrong from that point on. Not observed in the
wild; found while reading the code for Step 13a.

## A chunk that failed to load is dropped without a trace

`AsyncBuffer.fetchChunk` logs the failure and clears the fetching flag, so the
next read retries. That is the right behaviour for a hiccup and the wrong one
for a host that is gone: nothing counts the retries and nothing tells the
user.

## Nothing on screen says the editor is still catching up

A file opened at a saved position over a slow link sits at line 0 until the
background index reaches that line, and there is no sign anywhere that it is
going to move. The user cannot tell that a keystroke now costs them the jump,
and cannot tell the wait from an editor that has simply forgotten where they
were. A hint in the top bar while a restore is pending would cost very little.

## The restore still reads the file up to the saved line

The jump waits for the local index to walk every byte before the saved
position, which over FISH+ is round trips proportional to how deep in the file
the user was. `vfs.LineIndexer` already answers exactly this question in one
request — `lidx` on the remote host — and the viewer already uses it. The
editor could too, and then there would be no window to interrupt at all.

## A mouse click still cancels a pending restore unconditionally

`ProcessMouse` drops the restore whenever a button is down. A click does place
the cursor, so that is defensible, but it is not the same test the keyboard now
applies, and a click that lands on the cursor's own position cancels the jump
for nothing.

## The indexer's batch size is a constant nobody has measured

500 line offsets per batch, 64 KB per read. Both were picked to keep the UI
thread from being flooded by a local file, and neither has been measured
against a link where a read is a round trip.