# Ideas not yet on any plan

Things worth building that nobody is working on. Written down so they are not
rediscovered from scratch; nothing here is a commitment, and anything that
grows a plan of its own moves out of this file.

## Server side copy and move

Two panels on the same FISH+ host copy a file by pulling every byte to the
client and pushing it back. The remote host could do it itself: a copy is
`cp`, a move within one file system is `mv` and costs nothing at all. The
same holds for every other two panel operation. Only the numbers for the
progress dialog would have to travel.

The far more interesting case is two *different* hosts. If they can reach
each other — a key one of them accepts, or a password NetFox already knows —
then one of them can open its own FISH+ session to the other and act as a
proxy, and the bytes go directly between the two data centres at their own
link speed rather than through whatever the user is sitting behind. A move of
several gigabytes then finishes while the client is on a bad mobile
connection, because the client is only watching.

What has to be worked out: how the panel decides that two sessions are on the
same host (a host key is a better answer than a host name), how credentials
reach the far side without being handed to it in the clear, what happens when
the direct link between the two does not exist after all — falling back to
routing through the client has to be a decision the user sees rather than a
silent slowdown — and what the progress dialog shows for work it is not doing.

## One engine for running commands, not one per protocol

`vfs.CommandRunner` is the interface a panel asks; FISH+ answers it today
through its job machinery. SFTP will want the same thing, and SCP will when
NetFox grows it. All three sit on an SSH connection that can already run a
command — the differences are in bookkeeping, not in the running.

The temptation is to implement it three times. The cost of that is not the
duplicated code, which is small, but three different answers to the same
questions: when is a command finished, where does its standard error go, what
happens to it when the window is closed, what a non-zero exit means, how
output is delivered while it is still being produced. A user who moves
between two panels of different protocols would meet a different program in
each.

What the shared layer owns: the contract of `RunCommand` — output and errors
interleaved in the order they were produced, exit status reported rather than
turned into an error, cancellation stopping the command — and the plumbing
above it, which is the window, the job registry entry and the key binding.
What a backend owns: how the command is started and how its output comes
back. For FISH+ that is a job over the request stream, because the stream is
sequential and a command that prints for an hour would block a panel. For
SFTP and SCP it is an `ssh.Session` on the connection that is already open,
which needs none of that. `DialSSH` is already shared, so the SSH backends
would differ from each other in almost nothing.

Worth doing before the second implementation exists rather than after: two
implementations that already disagree are much harder to merge than one that
has not been written.
## Drag and drop, including to and from a remote host

f4 should support drag and drop, and it should work with a remote panel, not
only a local one. The natural place is an extension of the far2l extensions
protocol: the terminal already carries a side channel, and teaching it to
carry files would give a second way to move bytes without an sftp server —
useful well beyond drag and drop, and for exactly the hosts FISH+ was written
for.

## VTUI Applications as f4 Panels (F4 Panel Protocol)

A concept of running full-featured vtui applications (such as a Telegram client, a web browser, or any other app) either in standalone mode or directly embedded as an `f4` panel with full file operations, communicating with the other panel.

This could be achieved by using the file transfer protocol over far2l extensions (designed for drag-and-drop) or FISH+. Key architectural ideas:
*   **High-Speed Shared Memory Exchange:** Active panels and vtui applications running on the same host can exchange data instantly via shared memory.
*   **Fullscreen Overlay:** Embedded panels must be able to switch to a fullscreen overlay on top of regular panels on demand.
*   **Cross-App Panel Integration:** Using this protocol, other file managers and applications like Midnight Commander (`mc`), `far2l`, `far2m`, or `Far3` could be loaded as an active panel inside `f4`, and vice versa.
*   **SSH Tunneling:** This protocol should be routable over SSH. For instance, you could open a remote `mc` instance running on a router as a local `f4` panel.

## Using f4 as a Remote Server (Unified Helper)

While FISH+ normally deploys a POSIX shell helper script (`helper.sh`) on the remote side, we could also use `f4` itself as a remote server if it is already installed on the target machine.

This approach has massive benefits across all supported platforms:
*   It bypasses shell-specific limitations (like 32-bit arithmetic, lack of proper `dd`/`truncate` tools, or complex escaping).
*   It provides a high-performance, native Go-based server that speaks the FISH+ protocol out of the box over a simple SSH tunnel or TCP socket.
*   It simplifies remote host support, especially on non-POSIX platforms like Windows (see below).

## The credentials a reconnect needs

A FISH+ site opens through a dialer that holds the host, the user and the
password for as long as the panel is open, because a session that has to be
rebuilt has to be able to authenticate again. Before the reconnect work the
password was used once and forgotten — though it was read from a site
configuration on disk, so it was never a secret f4 alone was keeping.

Two things could make this better and neither is needed yet: asking the user
again instead of remembering, which turns a reconnect into a prompt and is
wrong for a panel that reconnects while nobody is looking, and holding the
authenticated `ssh.Client` rather than the password, which works for a
connection that is merely idle and not for one that is gone. Whichever way it
goes, it is a decision about credentials rather than about reconnecting, which
is why it is here and not in `STEP14.md`.
