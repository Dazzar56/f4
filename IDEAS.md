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

## Drag and drop, including to and from a remote host

f4 should support drag and drop, and it should work with a remote panel, not
only a local one. The natural place is an extension of the far2l extensions
protocol: the terminal already carries a side channel, and teaching it to
carry files would give a second way to move bytes without an sftp server —
useful well beyond drag and drop, and for exactly the hosts FISH+ was written
for.