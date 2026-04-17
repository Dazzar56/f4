# f4 Plugin Architecture (F4-RPC)

`f4` uses an **Out-of-Process RPC** architecture based on `stdin`/`stdout` and the **MessagePack** binary protocol.

## Why Out-of-Process?

In earlier versions, `f4` experimented with embedded WASM (`wazero`) and Lua (`gopher-lua`) interpreters to keep plugins strictly in-process while maintaining safety. This approach was abandoned due to several critical flaws:
1. **Binary Bloat**: Embedding multiple interpreters bloated the `f4` core binary.
2. **Platform Access Limitations**: WASM (WASI) severely restricts access to native OS capabilities (raw network sockets, complex file system operations), making it nearly impossible to write powerful plugins like process manager or Widows registry editor without building massive, fragile bridging layers.
3. **Language Lock-in**: Forcing developers to write in a specific dialect of Lua constrained ecosystem growth.

## The F4-RPC Solution

By moving to a separate-process model communicating over standard pipes:
1. **Zero Core Bloat**: The `f4` binary remains extremely lean.
2. **Language Agnostic**: You can write a plugin in Go, Python, Rust, C++, Node.js, or LuaJIT. As long as the process can read/write MessagePack to stdin/stdout, it works.
3. **Unrestricted Power**: Plugins run as native OS processes and can access the network, native libraries, and the full filesystem directly.
4. **Binary Efficiency**: We chose `MessagePack` over `JSON-RPC` to minimize serialization overhead and reduce data transfer latency for large `ReadDir` chunks.

## How it works

1. `f4` scans the `plugins/` directory for executable files.
2. It launches each executable via `os/exec`, hooking into its `stdin`, `stdout`, and `stderr`.
3. `stderr` is immediately piped to `f4`'s internal `debug.log` for easy debugging.
4. `f4` sends a `Plugin.Init` request over `stdin`.
5. The plugin replies with its capabilities (e.g., registered Virtual File Systems).
6. When a user interacts with the plugin's VFS, `f4` transparently routes `ReadDir`, `Stat`, `Open`, etc., as RPC calls.

## Building a Plugin (Go SDK)

For Go developers, an SDK is provided in `sdk/f4plugin`. It handles all the multiplexing and binary protocol details, exposing a clean, synchronous interface. Check `plugins/dummy/main.go` for a reference implementation.
