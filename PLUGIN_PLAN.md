# Plugin system overhaul: plan and rationale

This document exists so the work can be resumed from the repository alone. It
records what is being built, which decisions were made and why, and where the
work currently stands.

## Why

f4 exists partly because plugins in Far3, far2l and far2m are painful:

- they are not portable (the Lua plugin interfaces of Far3 and far2m are not
  fully compatible, and the DLLs are bound to the Windows API);
- binary plugins on Linux are bound to a libc version, which turns
  distribution into a mess, against a single f4 binary;
- far2l has no Lua at all;
- writing C or C++ is a high barrier to entry.

Nobody writes f4 plugins yet either. The likely reasons: f4 already ships with
batteries included, the barrier to entry is high and there is no getting
started guide, portability is unsolved (write in Lua and the target system may
not have Lua), and the API differs from Far3 so porting is a chore.

## The architecture

**One protocol, three transports.**

f4 already had an out-of-process plugin protocol: F4-RPC, MessagePack over
stdin/stdout, with method names like `Plugin.Init`, `VFS.ReadDir` and
`Host.Log`. Rather than growing a second and third plugin API for embedded
Lua and embedded wasm, all three transports carry that same protocol:

| transport | where the plugin runs | why it exists |
| --- | --- | --- |
| subprocess | its own process | any language, full OS access |
| embedded Lua | in f4, on gopher-lua | zero install, ships as one file |
| embedded wasm | in f4, on wazero | zero install, sandboxed, any source language |

The consequences are the point: one SDK surface, one set of documentation, one
set of host methods, and a plugin that moves between transports without being
rewritten. `PluginTransport` in `plughost.go` is the whole seam, a single
`Call(method, params, result)`; `newHostMethods` is the host side, shared by
every transport.

An earlier version of `PLUGINS.md` argued against embedded interpreters on
three grounds. They are answered rather than ignored: binary bloat is handled
with build tags, the sandbox's inability to reach native APIs is handled by the
FFI bridge, and language lock-in does not apply because the subprocess
transport never went away.

## Decisions and their rationale

1. **FFI is projected into the sandbox instead of wrapping host APIs.** Writing
   a wrapper for every Windows API function is not a plan. The plugin describes
   the ABI it wants and the broker performs the call. See `FFI.md`.

2. **A signature mini-language, not a C declaration parser.** `i64(str)` rather
   than `size_t strlen(const char *)`. A cdef-style front end can be layered on
   later without disturbing anything beneath it, and it is needed only for
   porting existing LuaJIT code.

3. **The Lua FFI module is named `f4ffi`, not `ffi`.** Ours is signature
   strings and raw addresses; LuaJIT's is `cdef` and `ffi.new`. Taking the name
   would make ported code fail late and confusingly rather than immediately and
   clearly. A future cdef front end can take the name honestly.

4. **The embedded runtime preloads `f4rpc`.** A plugin written against the
   subprocess SDK runs embedded without modification, because `require('f4rpc')`
   finds the preloaded module and never reaches the file that would drag in a
   MessagePack rock.

5. **Values cross the Lua boundary through MessagePack.** It costs a round trip
   but guarantees both transports agree on field naming, which is precisely
   where the older Far plugin APIs drifted apart.

6. **`print` is redirected into the host log, and `os`/`io` stay closed.** An
   in-process plugin writing to stdout would corrupt the screen f4 is drawing.

7. **One goroutine per Lua state, with an inline path for re-entry.** A native
   callback invoked on the runtime's own worker goroutine must not be queued or
   the worker deadlocks against itself.

8. **far2m's Lua API is the compatibility target, before Far3's.** The two
   share an ancestor in luafar, but far2m's is already free of Windows
   specifics. Implementing it gets most of Far3 for free; the remainder is a
   `winapi` shim later.

9. **No cgo, anywhere.** Portability is the reason f4 exists.

10. **The permission model is deliberately last.** This is alpha, and something
    working end to end is worth more than a model that is right the first time.
    Every dangerous operation already funnels through one hook, so adding it
    later is not invasive.

## Status

Done:

- **Step 1: `ffibridge`.** The FFI broker over pureffi. Signature parsing,
  dynamic dispatch through a runtime-built `reflect.FuncOf` type, a memory
  arena, callbacks, raw peek and poke, and a permission hook that is currently
  left open. Degrades to a stub under the `noffi` build tag.
- **Step 2: `luaplug`.** The embedded gopher-lua runtime: the `f4rpc` module,
  the `f4ffi` module, the sandbox, a worker goroutine per state and a deadline
  on every entry into the interpreter.
- **Step 2b: transports.** `PluginTransport` and shared `newHostMethods`;
  `LuaPlugin` mounts a Lua script as an in-process plugin. `plugins/dummy_lua`
  now runs without a system Lua.

Next, in order:

- **Step 3: Far-compatible Lua macros.** `Macro{area=, key=, description=,
  action=}`, `mf.*`, `Far.*`, `APanel`/`PPanel`, `Keys()`, scanning a macro
  directory. This lands on the existing `MacroManager`: the areas in
  `GetCurrentArea()` are already close to Far's, and `EventToFarString` and
  `ParseFarKey` already speak Far's key names. Keyboard-recorded macros keep
  working; the Lua engine is a second backend, not a replacement.
- **Step 4: embedded wasm on wazero.** wazero is already an indirect dependency
  through go-sqlite3 and needs promoting to a direct one. The hard part is
  pointers: a guest offset is not a host address. Guest linear memory is a Go
  slice with a real host address, so passing a pointer into it is possible
  without copying; the copy is only needed when the memory can grow or when
  native code retains the pointer past the call. Shared memory for heavy data
  is wanted, but not first.
- **Step 5: onboarding.** A scaffolder for new plugins, a five minute hello
  world, a PlugRing submission walkthrough, and a rewrite of `PLUGINS.md` and
  `LUA.md` to match reality.
- **Step 6: the permission model.** Manifest permissions with the author's own
  justification text, asked for on first real use, remembered in the config.
  Covers FFI, unsafe stdlib and running a native binary. Wires into
  `ffibridge.Options.Allow` and `luaplug.Options.AllowUnsafeStdlib`.
- **Later: a Far3/far2l API compatibility layer.** Explicitly deferred, but the
  namespacing choices above are made so it can arrive without a rewrite.

## Known issues

- A Lua plugin currently gets an FFI bridge with an open permission hook, so it
  can do anything f4 can. This is the alpha tradeoff recorded in decision 10.
- `Host.InputBox` and `Host.Menu` block until the UI answers. A plugin that
  calls them from inside a VFS request served on the UI goroutine will
  deadlock. This predates the embedded transports and affects the subprocess
  one identically.
- A runtime that has hit its call deadline was interrupted at an arbitrary
  instruction and should be discarded rather than reused.