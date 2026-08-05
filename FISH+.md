# FISH+: Enhanced Remote File Management

## The Concept

**FISH+** is an evolutionary step beyond the classic `fish` protocol (Files transferred over SHell) used in Midnight Commander and far2l. While standard `fish` uses simple shell commands (ls, cat, dd) to simulate a file system over SSH, **FISH+** aims to minimize network traffic and latency by offloading heavy processing to the remote server.

## Architectural Advantages

Traditional remote file systems (SFTP, NFS, SMB) treat the server as "dumb storage," requiring the client to download data to process it. FISH+ treats the server as a "remote worker."

### 1. Remote Search (Server-Side Grep)
Instead of downloading a 1GB log file to search for a string, `f4` sends a search request to the FISH+ handler. The server runs a native `grep`-like process and returns only the byte offsets of the matches. The VFS then fetches only the relevant chunks for display.

### 2. Remote Indexing
Calculating line breaks for a large file is expensive over a network. With FISH+, the server-side script calculates the `LineIndex` locally and sends the array of offsets to the client. This allows `f4` to open a multi-gigabyte file over SSH and allow instant jumping to the end or middle of the file.

### 3. Delta-Based Editing (Sparse Saving)
The `PieceTable` model used in `f4` is essentially a list of edit instructions (insert X at Y, delete range A-B).
*   **Classic SFTP:** To save a 1-byte change in a 100MB file, you must re-upload the entire 100MB.
*   **FISH+:** `f4` sends only the `PieceTable` deltas. A small server-side script applies these changes to the remote file in-place.

### 4. Background Offloading
Operations like calculating directory sizes, finding duplicate files, or complex pattern matching are executed as remote background tasks. The FISH+ VFS reports progress back to the `f4` Progress Dialog without saturating the network with raw file data.

## Code Layout

*   `plugins/netfox/fishplus/` — the protocol client. It depends on the standard library only and knows nothing about SSH: it talks to any duplex byte stream, which makes it testable against a local `/bin/sh`.
    *   `helper.sh` — the shell helper that is uploaded to the remote host on every connect.
    *   `script.go` — embedding, token substitution and compaction of the helper.
    *   `session.go` — handshake, request/response framing, feature detection.
    *   `fs.go` — listing and metadata parsers for every backend.
    *   `read.go` — ranged reads and the cached file handle.
    *   `write.go` — ranged writes, truncation and the buffering write handle.
    *   `mutate.go` — directory and metadata mutations.
*   `plugins/netfox/fish_vfs.go` — the `vfs.VFS` implementation, registered as the `fish+` protocol of the NetFox connection manager. A plain `fish` type is accepted as a synonym.
*   `plugins/netfox/ssh_dial.go` — the SSH dialer shared with the SFTP backend.

### Licensing

The helper script and the wire format are written from scratch for f4 (BSD-3-Clause). Nothing is copied or adapted from far2l or Midnight Commander, which are GPL licensed. Their fish implementations are used as a source of ideas only.

## Wire Protocol, Version 1

### Session bootstrap

The client starts a plain POSIX shell on the remote host, writes the compacted helper script into its stdin and then keeps talking over that very same pair of streams. When the client closes its side, the helper's `read` hits EOF and the remote shell exits on its own — no farewell command is needed, so a hung remote can never block the UI thread.

A shell that ended up on a pseudo terminal needs one more step. A terminal echoes back everything it is fed, turns every `\n` on the way out into `\r\n` — which destroys binary frames — and in canonical mode truncates an input line at a few kilobytes, which would cut off a long path. So the helper checks whether its stdin is a terminal and, if it is, puts it into a transparent mode with POSIX `stty` operands only, announcing `tty` among its features. A client whose transport is a terminal and which does not see `tty` in the banner must treat binary payload as unsafe.

### Request

One line per request, optionally followed by one line per path the command works on:

    <id> <cmd> [<short arg> ...]
    <path>

`id` is a decimal counter that starts at 1 and never repeats within a session. Short arguments are bare tokens: they must not be empty and must not contain whitespace, which is why every path travels on a line of its own, read by the helper with `IFS= read -r`. A path therefore reaches the remote host byte for byte — spaces, leading and trailing ones included, tabs, backslashes, quotes, dollar signs, any encoding.

Only a path that a line based channel cannot carry — one containing a newline, or one starting with the escape marker itself — is base64 encoded and prefixed with `~`. That keeps the classic fish trade-off the right way round: raw whenever possible, base64 only where nothing else works, so the remote host does not fork a decoder per request.

### Response

Zero or more payload lines, then a terminator:

    .<token> <id> ok|err [message]

`token` is 64 bits of randomness generated by the client for each session and substituted into the helper before upload. That is what makes the terminator unambiguous: output of `ls`, `stat` or `grep` cannot accidentally look like one.

Binary payload is sent in frames, each being a header line followed by exactly `n` raw bytes:

    #<n>

Frames are only recognized for commands the client issued as data commands (`ExecData`), so a text payload line starting with `#` stays a text line.

### Handshake

The helper answers with a terminator carrying the reserved id `0`:

    .<token> 0 ok FISHPLUS 1 dd base64 readlink du grep sed awk wc sha256sum stat find

Everything printed before that line (motd, shell warnings, login banners) is discarded by the client. Such noise does not always end with a newline, and neither does the echo of the uploaded script on a pseudo terminal, so during the handshake — and only there — the client looks for the terminator anywhere in the line rather than at its start, while the helper prints a newline of its own before the banner. The word list after the version is the set of features the helper detected on the remote host; later steps pick their strategy from it (`stat` vs `statbsd` vs `find` vs plain `ls`, `dd` availability and so on). A failing host answers `err` with a reason instead.

### Commands implemented so far

*   `noop` — cheapest possible round trip.
*   `pwd` — the directory the remote shell started in.
*   `ping` + payload line — echoes the payload back; keepalive and synchronization check.
*   `feats` — repeats the version and feature list.
*   `enum` + path — lists a directory.
*   `info` + path — metadata, following symlinks.
*   `linfo` + path — metadata of the link itself.
*   `rdlink` + path — the target of a symlink.
*   `read <offset> <length>` + path — a byte range, `length` zero meaning "to the end of the file".
*   `write <offset> <length> raw|b64` + path + payload — the same range in the other direction; the payload follows the path line and is described below.
*   `trunc <size>` + path — sets the size of a file, creating an empty one where there was none.
*   `mkdir` + path — creates a directory and its missing parents.
*   `rm`, `rmdir`, `rmtree` + path — a file, an empty directory, a whole tree.
*   `mv` + two paths — the first command that reads more than one path line.
*   `chmod <octal>` + path — the mode is checked for being octal before it reaches the remote `chmod`.
*   `chown <uid> <gid>` + path — either half may be `-`, meaning "leave it alone".
*   `utime <mtime> <atime>` + path — epoch seconds, either of them `-` for "leave it alone".
*   `grep <mode> <limit>` + pattern + path — byte offsets of the matches, at most `limit` of them.
*   `lidx <first> <count>` + path — where the given lines start, and how many lines the file has.
Every mutation refuses a path that is not absolute or that carries a `..` component, and the root directory itself. The client always sends absolute paths, so the rule costs nothing in normal use; it is there because `rmtree` turns one mistake in path assembly into a lot of lost data, and because a check the remote host performs holds even when the client is the thing that went wrong. A name that merely begins with dots is not a `..` component and stays usable.
*   `mode <name>` — forces a metadata backend instead of the auto-detected one; for tests and for troubleshooting.
*   `rmode <name>` — the same for the read backend.
*   `wmode <name>` — the same for the write backend.
*   `exit` — makes the helper leave its loop.

### Metadata backends

Listing is where remote hosts differ the most, so the helper probes what is there and reports the winner as `mode:<name>` among its features. In order of preference:

1.  **find** — `find -H DIR -mindepth 1 -maxdepth 1 -printf '%y %Y %s %T@ %A@ %C@ %m %U %G %f\n'`. One process for the whole directory, no shell glob, hidden entries included, sub-second timestamps, and `%Y` tells for free whether a symlink resolves to a directory. GNU only.
2.  **stat** — `stat -c '%f %s %Y %X %Z %u %g %n' -- DIR/* DIR/.*`. Needs a shell glob, so a huge directory can hit the argument limit, and the type of a symlink target stays unknown until someone asks for it.
3.  **statbsd** — the same shape with `stat -f '%p %z %m %a %c %u %g %N'` for BSD and macOS.

A pure `ls -l` fallback for hosts that have neither is a separate step; it needs real output from such hosts, which is what the compatibility issue and `tools/fishplus_probe.sh` collect.

The first payload line of a listing names the backend that produced the rest, so the client always knows which parser to use — including when the backend was switched at runtime.

### Reading

`read` answers with a text line `S <size>` carrying the size the remote host saw, then one binary frame with the bytes that were actually available in that range. The size travels along because a client following a growing log file needs it anyway, and because it is what the helper used to decide how much to send.

Which tool does the reading is detected as well, and reported as `read:<name>`:

1.  **ddbytes** — `dd bs=1M iflag=skip_bytes,count_bytes skip=OFF count=LEN`. Any offset, any length, one process. GNU and recent BusyBox only.
2.  **dd** — plain `dd bs=BS skip=OFF/BS count=LEN/BS`, where `BS` is the largest power of two up to 64k that divides both the offset and the length. A client that asks for chunk aligned ranges — which `File` does — therefore gets whole block reads and a single `lseek` on BSD, macOS and Solaris too, without the GNU only `iflag`.
3.  **tailc** — `tail -c +OFF | head -c LEN`, for hosts with no usable `dd`.
4.  **cat** — whole files only; a byte range is refused rather than answered with the wrong bytes.

The offset is clamped against the size the helper just read, so the frame length is exact and the stream stays in sync.

### Writing

The payload of a `write` follows the path line and carries no terminator of its own: the helper reads exactly as many bytes as the request announced. One byte too few or too many and the remote shell parses the rest of a file as commands, so the entire design of this step is about who counts those bytes and what happens when the count cannot be kept.

Which tool does the counting is detected during the handshake and reported as `write:<n>`:

1.  **ddbytes** — `dd bs=1M iflag=fullblock,count_bytes count=LEN oflag=seek_bytes seek=OFF conv=notrunc`. One process, any offset, any length; `fullblock` is what makes it stop on the exact byte while still reading whole blocks, because a short read from a socket must not end the copy. GNU and recent BusyBox only.
2.  **b64** — the client sends a single line of base64 and the helper reads it with the shell's own `read`, which cannot overshoot: a line ends where its newline is. Positioning is then a plain `dd` on the largest power of two dividing both the offset and the length, the same arithmetic the read path uses. It costs a third more traffic and is still the better choice on a host without GNU dd.
3.  **ddbs1** — `dd bs=1 count=LEN seek=OFF conv=notrunc`, exact because a one byte read cannot come back short, but a syscall per byte. It is never picked automatically; `wmode` selects it for a host where the other two misbehave.

A range starting past the end of the file leaves a hole, and nothing after the written range is touched — which is what the delta based saving of step 8 will need.

`trunc` sets a size. Zero is a shell redirection and therefore works everywhere, including on a file that does not exist yet; any other size needs the `truncate` utility, announced as the `truncate` feature. `Create` truncates to zero and then writes forward, so the common case never depends on it.

#### When a write fails

A refusal the helper can see coming — a directory, a path it may not write, a bad range — is answered only *after* the payload has been read into `/dev/null`, because a failed request must still leave the stream where the next request expects it. Such a reply carries a `D` line and the client treats the session as healthy.

A write that dies in the middle — the disk fills up, the medium fails — leaves an unknown number of bytes on the wire, and no amount of guessing on the client side can recover them. There is no `D` line then, and the client marks the session broken so that it gets reconnected instead of quietly writing the rest of the payload into the next file. Step 10 is where a broken session learns to resynchronize instead.
### Ownership and timestamps

`chown` is the easy half: a numeric `uid:gid` pair, either side of it droppable, and one utility that behaves the same everywhere.

Timestamps are not. GNU `touch` takes an epoch as `-d @1400000000`; BSD and macOS have never accepted that and want `-t YYYYMMDDhhmm.SS` — in the *local* time of the host, which is exactly the one thing the client cannot compute for it. So the conversion happens on the remote side: the helper tries GNU form first, and if it is refused it asks the host's own `date` to render the epoch, `date -r` for BSD and `date -d @` for GNU, and feeds the result to `touch -t`. The `date -r` attempt is skipped when a file with that name exists in the current directory, because GNU `date` reads `-r` as "reference file" and would then quietly report that file's time instead.

Setting both times is one `touch`; setting them to different values is two, `-m` and then `-a`, because that is all POSIX `touch` offers. A caller that wants neither pays no round trip at all.
### Remote search

This is the first command that is FISH+ rather than fish: the remote host does the work and only the answer travels. `grep -a -b -o` prints one match per line as `offset:text`, and an `awk` behind it throws the text away and stops after the limit, so a pattern matching a million times costs the same handful of bytes on the wire as one matching three times — and grep dies on the broken pipe instead of reading the rest of the file.

The mode argument is `f` for a fixed string or `e` for an extended regular expression, with an `i` appended to fold case. Pattern and path both travel on lines of their own, escaped the same way as any path, so a pattern with spaces, tabs or a leading `~` arrives byte for byte.

`FishVFS.Search` uses it with a fixed pattern and announces `HasSearch` accordingly. A host without `grep` or without `awk` answers `nil`, which is f4's way of saying "search it yourself by reading" — the same fallback SFTP gets.
### The remote line index

Browsing a huge remote file in pieces already works: the viewer renders from a byte offset and asks the VFS for what it needs through `ReadAt`, so a panel opens the head of a gigabyte log instantly. What does not work that way is anything that has to know where the *lines* are — jumping to the end, jumping to line number N, showing how many lines there are — because finding that out means walking the file, and walking it over the network means downloading it.

`lidx` moves that walk to the far side. One `awk` pass prints the byte offset of each requested line and, at the end, `T <total>`. What crosses the network is a handful of numbers no matter how large the file is. The offsets are byte offsets because that is the only currency the rest of the protocol speaks: feed one straight back into `read`, and the viewer is drawing that line.

The pass is a full one — awk cannot know where line N starts without counting the newlines before it — so the cost is one sequential read on a machine that has the file locally, instead of one transfer across the network. That trade is the whole idea of FISH+.
### Known limitations of v1

*   The remote host must provide a base64 decoder (`base64` or `openssl`), even though almost no request needs one: the handshake refuses hosts without it because a path with a newline would otherwise be unreachable. Dropping that requirement for hosts that never see such a path is possible later.
*   A file truncated between the moment the helper reads its size and the moment `dd` runs makes the frame shorter than its header promises, which desynchronizes the session. Growth is harmless; truncation under the reader is not, and detecting it costs a second pass over the data.
*   The helper does its size arithmetic in the shell, so a host whose shell has 32 bit arithmetic cannot address past 2 GB. `tools/fishplus_probe.sh` reports this per host.
*   A listing carries file names on a line each, so a name containing a newline shows up truncated in a directory listing, exactly as in classic fish. Operating on such a file still works, because paths going the other way are escaped.
*   Cancelling a request while its response is being read desynchronizes the stream, so the session is marked broken and has to be reconnected. Proper mid-request cancellation is a separate step of the plan.
*   Two panels sharing one session — which is what `Clone` gives them — take turns rather than working in parallel, because the remote shell answers one command at a time. That also means a cancellation in one panel breaks the session for the other until step 10 makes cancellation recoverable.
*   `SetAttributes` follows the SFTP backend: a `Uid` or `Gid` of -1 means "leave the ownership alone", and a zero-valued `VFSItem` therefore asks for `chown 0:0` rather than for nothing. Callers with nothing to change have to say so with -1, which is what the copier does.
*   In the `stat` and `statbsd` backends a directory is listed through a shell glob, so a directory with very many entries can exceed the argument limit, and a symlink is reported without the type of its target until the user enters it.
*   Hosts with neither GNU find, nor GNU stat, nor BSD stat cannot be listed at all yet.
*   A host without `dd` cannot be written to at all, and the client refuses to send a payload it knows the remote side cannot take off the wire.
*   The `b64` backend puts a third more bytes on the wire and makes the remote shell read them one syscall per byte, which is why the client sends it smaller chunks than a raw backend.
*   Shrinking a file to a non-zero size needs the `truncate` utility; without it only truncation to zero is possible.
*   A raw payload assumes a binary safe transport. The ssh backend asks for no pseudo terminal, so this holds there; a caller supplying its own terminal backed stream has to select `wmode b64` by hand for now.
*   A host with a `touch` that takes neither `-d @epoch` nor `-t`, or with no `date` able to render an epoch, cannot have its timestamps set. The helper says so instead of writing the wrong time.
*   Times are set with a resolution of one second, because that is what `touch -t` carries. The sub-second part a listing reports is therefore lost on a copy.
*   `chown` follows symlinks, so setting the ownership of a symlink changes its target instead. Doing it the other way round needs `chown -h`, which not every host has.
*   A search is capped at 10000 matches and the client cannot tell a file with exactly that many hits from a truncated answer. A count line would fix it and costs a second pass over the file, so it waits for a case that needs it.
*   Case folding and regular expression dialect are the remote `grep`'s, not Go's, so a search over a FISH+ panel can match slightly differently than the same search over a local one. That is the price of not moving the file.
*   `grep` cannot match across a line break, so a pattern containing a newline finds nothing. The viewer's search will have to split such a pattern itself.
*   `lidx` reads the whole file to answer, so on a slow remote disk the first jump to the end of a very large file takes a while, even though nothing is transferred. Caching the total per file and reusing it belongs with the viewer wiring.
*   A line index counts `\n` only. A file with classic Mac line endings is one line to `lidx`, exactly as it is to `awk`, `grep` and `wc` on the remote host.

## Roadmap

### Done

*   **Step 1 — transport and protocol core.** Helper script, handshake, feature detection, request/response framing, base64 arguments, binary frames, error reporting, session teardown. Tested both against an in-memory peer and against a real local shell.
*   **Step 2 — listing and metadata.** `enum`, `info`, `linfo`, `rdlink` and the runtime `mode` switch, with the find, GNU stat and BSD stat backends and their parsers. Paths now travel raw instead of base64. The integration test drives every backend the test machine provides, over names with spaces, tabs, backslashes, trailing blanks and non-ASCII characters.
*   **Step 3 — reading.** `read` with an offset and a length, raw binary frames with no base64 in the way, four read backends with runtime switching through `rmode`, and a `File` handle with a chunk cache that satisfies `vfs.ReadAtCloser`. The terminal safeguards and the first round of compatibility fixes from issue #316 landed here as well.

*   **Step 4a — the VFS.** `plugins/netfox/fish_vfs.go` maps `Entry` onto `vfs.VFSItem` and a `fishplus.File` onto `vfs.ReadAtCloser`, so a FISH+ session is already a browsable and readable file system. The helper learned `pwd`, so a panel opens where an interactive login would land. Mutations answer with a plain error until step 5, and the test drives the whole mapping over a local shell.

*   **Step 4b — transport and registration.** `DialSSH` now carries the agent, key and password logic for both SSH backends, and a FISH+ site opens a shell with no pseudo terminal attached, running `exec /bin/sh` so that a csh or fish login shell cannot get in the way. The protocol is registered as `fish+`, with the plain `fish` type accepted as a synonym.

*   **Step 5a — mutations.** `mkdir`, `rm`, `rmdir`, `rmtree`, `mv` and `chmod`, and the `FishVFS` methods on top of them. A recursive delete is one round trip rather than one per entry, because the remote host does the walking. `Create` is now the only method that still refuses.
*   **Step 5b — writing file content.** The `write` command with an offset and a length, three write backends with runtime switching through `wmode`, `trunc`, and a buffering `Writer` on the client side. A refused request still drains its payload and says so with a `D` line; a write that fails halfway does not, and marks the session broken rather than letting it desynchronize.
*   **Step 5c — the writing VFS.** `FishVFS.Create` on top of `Writer`, plus `chown` and a `touch` that works on GNU and on BSD alike by letting the remote host convert the epoch into its own local time. `SetAttributes` now carries mode, ownership and timestamps, so a file copied onto a FISH+ panel arrives with the attributes it had. Nothing in the VFS refuses any more, and `ErrFishReadOnly` is gone.
*   **Step 7a — remote search.** The `grep` command, `Client.Grep` and `FishVFS.Search`, with `HasSearch` finally true for one of f4's file systems. Only byte offsets cross the network, and the limit is enforced on the remote side rather than by disconnecting a flood.
*   **Step 7b — the remote line index.** The `lidx` command and `Client.Lines`: the offsets of a range of lines and the total line count, from one remote `awk` pass and a few numbers on the wire.

*   **Step 5d — a `Close` whose error is not dropped.** `closeOnce` lets the three places that write through a VFS close explicitly where the error matters and still keep a `defer` as a safety net. `recursiveCopy` now closes the destination before declaring success, so a failed last chunk removes the incomplete file instead of reporting a copy that did not happen; the download to the temporary file and the upload back after an external editor report it too. None of this is specific to FISH+, it affects every file system that buffers.

### To do

The order below is chosen so that something usable arrives as early as possible: after step 4 a user can already browse, view and download.

*   **Step 6 — odd hosts.** The `ls -l` fallback backend and whatever else the compatibility issue turns up; `tools/fishplus_probe.sh` collects the raw material.
*   **Step 7c — the viewer on top of it.** `ViewerView` already renders from a byte offset through `ReadAt`, so a remote file is browsed in pieces today; what it still does by reading is jumping to the end and counting lines. Wiring those to `Client.Lines` needs a VFS-level hook, since `vfs.VFS` has no line index method yet, plus a per-file cache of the total so that one keystroke does not start a new remote pass. That interface question is what makes it a step of its own.
*   **Step 7d — file search function also should be offloaded to server.
*   **Step 8 — FISH+ proper, part 2.** Delta based saving: PieceTable edits applied remotely instead of re-uploading the file.
*   **Step 9 — FISH+ proper, part 3.** Background jobs (directory sizes, duplicate search, hashing) reporting progress through the f4 progress dialog.
*   **Step 10 — resilience.** Mid-request cancellation and resynchronization without dropping the session, keepalive, automatic reconnect.
*   **Step 11 — remote execution.** `exec` and a remote terminal, plus user documentation and help pages.

## Testing

    go test ./plugins/netfox/fishplus/ ./plugins/netfox/

The tests whose names end in `AgainstLocalShell`, together with `TestFileReadAtAndCache` and the `FishVFS` tests in the `netfox` package, run the real helper in a local `/bin/sh`. That is the only kind of test that proves the shell side and the Go side agree on the wire format, and it walks every backend the test machine provides, one subtest each. They skip themselves on Windows and on hosts without a shell or without base64.

Every write test ends with a `ping` on purpose. Only what the session answers *after* a refused write can show that the payload really left the wire; a test that just checks the error would pass on a helper that desynchronizes the stream.
The timestamp tests run twice: once against the tools the machine has, and once with stubs in front of `touch` and `date` that refuse `-d` and answer `-r`, the way macOS does. Without the second run the BSD branch would never be executed on a GNU build machine, and a mistake in it would only surface on a user's Mac.
### What the compatibility issue changed

Issue #316 collects probe output from hosts we do not own. The first three reports (macOS 26 on arm64, Git for Windows/MSYS2, Ubuntu under WSL2) already paid for themselves:

*   `openssl base64 -d` decodes *nothing* when its input does not end with a newline, which made the openssl fallback dead weight: a host whose `base64` speaks neither `-d` nor `-D` was refused at the handshake. The decoder probe and `f4_dec` now both terminate their input, and openssl is called with `-A` so a long line survives.
*   macOS `head -c` reads its input to the end even after it stopped writing, so it can never be used on a stream someone else still has to read from. The helper probes for this and reports `headc` and `headsafe` separately; the write step will need it.
*   macOS `find` has no `-printf` and its `stat` no `-c`, so `statbsd` is the backend there, and its `dd` has no `iflag`. Both paths are exercised here against a simulated host built from that report.

Hosts with neither GNU find nor either `stat` are still unlisted; the `ls -l` fallback waits for probe output from the systems that need it. Probe version 2 asks for what that parser will need — `ls -lT`, `--time-style`, `--full-time`, numeric ids, symlink rendering — plus the `dd`, `tail -c`, `stty` and shell arithmetic details the read and write steps depend on.