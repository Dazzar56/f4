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

## Implementation Path
FISH+ will be implemented as an internal VFS plugin that executes a bootstrap shell script on the remote machine via SSH. This script then acts as a multiplexer for the optimized operations described above.