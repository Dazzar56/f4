# Mount performance: the baseline

Numbers taken with `fusefs/bench-all.sh` before the locking in `bridge.go` is
touched, because iteration 6 is gated on being able to show that a change
moved something. A number without its fixture and its link means nothing, so
both are recorded.

Fixture: one 8 MiB file, 500 small text files, one nested path. The same
content in all three sources, so the differences are the backend.

## 2026-08-13, f4 ab68a5f, Linux 6.8

| | local dir | archive (tar) | sftp, fast link | sftp, mobile link |
|---|---|---|---|---|
| sequential read (`dd`) | 0.01s | 0.02s | 0.60s\* | 9.45s |
| walk + read all (`tar -c`) | 0.01s | 0.01s | 0.01s\* | 2.37s |
| stat every entry (`ls -lR`) | 0.01s | 0.02s | 0.01s\* | 0.01s |
| search (`grep -rlI`) | 0.06s | 0.56s | 1.74s\* | 156.75s, failed |

\* the fast-link column was taken with the older 64 MiB fixture, so its
sequential read is not comparable with the rest of its column; the shape is.

## What the baseline already says

**Metadata is free, everywhere.** `ls -lR` over a remote host on a mobile
connection costs the same as over a local directory. The directory cache and
the kernel's attribute cache are doing exactly what they were put there for,
and there is nothing to win in that column.

**Bandwidth is bandwidth.** `dd` tracks the link and nothing else. No amount
of locking policy changes it.

**The search is the outlier, and it is not bandwidth.** On the mobile link,
`tar -c` reads every byte of the fixture in 2.37s while `grep` over the same
files takes 156s and then fails. Two orders of magnitude between two commands
reading the same data is not a slow link; it is something about how the mount
answers many small opens, and it is reproducible on that machine while being
absent on the fast one.

That is the thread worth pulling before touching any mutex: a lock that is
held per call cannot explain a 65x gap against a single-threaded reader, so
either the per-open cost is much higher than `tar` makes it look, or something
in the read path is doing far more work than the request needs.

## Next

* Find out what the search actually does differently — per-file open cost, or
  a cache that expires under it, or an error it recovers from 500 times.
* Only then decide whether the single bridge lock is worth replacing, and
  re-run this script to say so with numbers.
* The benchmark has no concurrent case yet. The lock is about parallel load,
  so a fifth measurement — two readers at once — has to exist before the
  locking change can be judged at all.
