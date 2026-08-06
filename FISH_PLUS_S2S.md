# FISH+ Server-Side & Server-to-Server (S2S) Operations

## Overview

**FISH+** in `f4` includes native optimizations for high-performance remote file management: **Server-Side** and **Server-to-Server (S2S)** Copy and Move.

Traditional file managers treat remote servers as "dumb storage," requiring the client to pull every byte to the local machine and push it back, causing high latency and network saturation. `f4` treats remote servers as "remote workers." By orchestrating operations directly on the remote hosts, files travel at local disk or datacenter-to-datacenter speeds, completely bypassing the client.

---

## 1. Server-Side Copy and Move (Same Host)

When you copy or move files between different folders on the *same* remote host (even if the panels were opened as separate connections), `f4` performs the operation natively on the server.

*   **How it works:**
    *   For **Move/Rename** operations, `f4` executes a server-side `mv`.
    *   For **Copy** operations, `f4` executes a server-side `cp -R`.
*   **Benefits:**
    *   **Instantaneous execution:** A 10GB file is copied or moved in milliseconds.
    *   **Zero bandwidth usage:** Not a single byte of file content travels through your local connection.
    *   **Preserves UI/Overwrite flow:** `f4` recurses file-by-file, ensuring that individual file progress bars, speedometers, and standard overwrite/skip confirmation dialogs work exactly as they do on a local drive.

### Requirements:
Simply open both the left and right panels to the same host using FISH+ (the connection titles, e.g. `user@host:port`, must match).

---

## 2. Server-to-Server Copy and Move (S2S)

When copying or moving files between *two different* remote hosts (Host A and Host B), `f4` leverages the network link between the two servers.

*   **How it works:**
    `f4` spawns a managed background process on Host A (the source) that copies files directly to Host B (the destination) using the standard secure copy utility:
    ```bash
    scp -P <port> -o StrictHostKeyChecking=no -p "/src/file" user@hostB:"/dst/file"
    ```
*   **Benefits:**
    *   **Datacenter speeds:** Files travel over high-bandwidth datacenter links instead of saturating your home or office connection.
    *   **Non-blocking and cancellable:** The transfer runs as a managed remote job. If you click **[ Cancel ]** in the f4 progress dialog, the client instantly sends a `jkill` signal to Host A, killing the remote `scp` process immediately.
    *   **Metadata preservation:** The `-p` flag in `scp` automatically preserves modification times, access times, and mode permissions.

### Authentication & Security:
Since Host A must authenticate with Host B, `f4` supports two seamless authentication paths:

1.  **Server-to-Server Keys (No configuration needed):**
    If Host A is already authorized to connect to Host B (i.e. Host B's `authorized_keys` contains Host A's public key), the transfer succeeds out of the box.
2.  **SSH Agent Forwarding (Secure local key delegation):**
    If your private keys are stored only on your local desktop machine, `f4` automatically forwards your active local `ssh-agent` to Host A. Host A can then authenticate with Host B using your local keys *without* those keys ever being exposed or written to Host A's disk.

---

## 3. How to Use It

1.  Ensure you have your SSH keys added to your local agent (optional, only needed if using SSH Agent Forwarding):
    ```bash
    ssh-add ~/.ssh/id_rsa
    ```
2.  Open `f4`, press `Alt+F1`/`Alt+F2`, and open your FISH+ sites on the left and right panels.
3.  Select the files/folders you want to transfer.
4.  Press `F5` (Copy) or `F6` (Move).
5.  Confirm the operation. The files will be transferred directly between the hosts!

---

## 4. Robust Fallback System (Fail-Safe)

Portability and reliability are the core goals of `f4`. Because different remote environments can have restrictive firewalls, custom security policies, or missing utilities, the entire server-side system is designed with a **fail-safe fallback**:

*   If a server-to-server copy command (`scp`) fails (e.g. because port 22 is blocked between the servers, or keys are not authorized), `f4` catches the error.
*   It logs the error to `debug.log` and **automatically falls back to standard client-side streaming** (pulling/pushing bytes through the client).
*   This guarantees that your files are always copied successfully, no matter how restrictive the environment is.