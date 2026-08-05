# Step 14b — reconnecting a FISH+ session

Step 14a is done: an idle session is held open by a `noop`, and a session that
stops answering is marked broken twenty seconds later instead of at the user's
next keystroke. What is left is picking the session back up.

This file is the design, written before the code so the next session starts
from a decision rather than from an analysis. It is deleted when step 14b
lands and its conclusions move into `FISH+.md`.

## What a reconnected session actually is

A new shell on the far side. It has a new pid, a new job directory, a new
token, and it knows nothing of what the old one was doing. Every honest
answer here follows from that one sentence, and every dishonest one comes from
pretending otherwise.

What survives, because it lives here and not there:

*   The path a panel is standing in. `FishVFS.path` is a client-side string.
*   A `File` handle. It holds a cache and an offset and nothing else — the
    comment on it already says so. Its next `ReadAt` is a fresh ranged read.
*   The credentials, which `DialSSH` used once and can use again.

What does not survive, and must not be papered over:

*   Background jobs. The helper's trap kills them when the shell dies, and
    what it does not kill is a `find` on somebody's server with nothing left
    to notice it by. A reconnect must report them as lost, not silently
    restart them: a scan that started again from the beginning would report
    numbers the user never asked for.
*   A `Writer` mid-file. Its buffer is here, but the bytes already sent are
    there, and after a drop nobody knows how many arrived. Resuming would
    write a file with a hole in it. It fails, and it says why.
*   A `patch` in progress, for the same reason and with the same answer: the
    remote `.f4tmp` may or may not exist and may or may not be complete.
*   The remote working directory, which is why `pwd` is asked again and the
    panel's own absolute path is used rather than trusting where the new
    shell landed.

## The shape

`fishConn` is the right owner. It already holds the client, counts the users
and decides when the session dies; it is the only thing that sees a session
rather than a view of one.

*   It gains a dialer — a `func(ctx) (streams, closer, error)` handed in by
    whoever built it, so `fishplus` keeps knowing nothing about SSH and the
    tests keep reconnecting to a local shell.
*   `NewFishVFSOnStream` has no dialer and therefore cannot reconnect. That is
    correct: a caller who handed over a pair of streams has no second pair.
*   A reconnect replaces `fishConn.client` under its mutex. Everything else
    reaches the client through the conn, so nobody holds a stale pointer.

## When it happens

Not automatically inside a request. A request that reconnects on its own turns
one failure into a delay of unknown length in the middle of a copy, and the
user has no way to say no.

Instead: the session is marked broken as it is now, and the *next* operation
that meets `ErrBroken` asks. One dialog, three buttons — reconnect, work
offline, close the panel — and a reconnect that succeeds retries exactly the
operation that asked. An operation that cannot be retried honestly (a write, a
patch, a job) says so in the dialog rather than offering a retry that would
corrupt something.

The keepalive is what makes this bearable: without it the question arrives an
hour late, attached to whatever the user happened to do next.

## Order of work

1.  ~~`fishConn.reconnect`: dial, handshake, swap the client, one round trip to
    confirm.~~ Done. `FishDialer` is
2.  ~~The dialer plumbed in from the FISH+ site~~ — half done. `FishVFS.
    Reconnect` and `CanReconnect` are the entry point a caller uses, and
    `NewFishVFSOnDialer` is what a site has to be opened through. What is left
    is the site itself: whatever builds a FISH+ panel from a NetFox
    configuration still calls `NewFishVFSOnStream`, so it opens a session that
    cannot be rebuilt. That change is in `ssh_dial.go` and the FISH+ site
    constructor, and it is the next thing to do.

    A related mechanical change belongs with it: every `FishVFS` method reaches
    the session through its own `v.client` field, so only the view that
    reconnected is repointed. `Clone` already asks the connection instead, so a
    view born after a reconnect is on the live session; the request methods are
    what is left, and each of them becomes `v.conn.current()`.
3.  The dialog, and the retry of the operation that raised it. Read-only
    operations retry; the rest report.
4.  Jobs: `jlist` on a fresh session comes back empty, so the registry is told
    its remote entries are gone. That is a UI change and belongs last.

## What stays out of scope

Resuming a transfer where it stopped, and a session outliving the process, are
both listed under step 14 in `FISH+.md` and both need more than a reconnect:
the first needs the remote side to say how much it received, the second needs
state on disk that survives f4 being closed. Neither is blocked by this work
and neither should be folded into it.